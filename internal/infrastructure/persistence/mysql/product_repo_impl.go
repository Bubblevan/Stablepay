// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package mysql implements repository.ProductRepository for MySQL.
package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/stablepay/merchant-server/internal/domain/entity"
)

// ProductRepoImpl implements repository.ProductRepository backed by MySQL.
type ProductRepoImpl struct {
	db *sql.DB
}

// NewProductRepo opens a MySQL connection and runs auto-migration.
func NewProductRepo(dsn string, autoMigrate bool) (*ProductRepoImpl, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("mysql product repo: dsn is required")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("mysql product repo: open: %w", err)
	}

	// Sensible connection pool defaults (same pattern as other StablePay services)
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(1 * time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mysql product repo: ping: %w", err)
	}

	repo := &ProductRepoImpl{db: db}
	if autoMigrate {
		if err := repo.Migrate(ctx); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return repo, nil
}

// Close shuts down the database connection pool.
func (r *ProductRepoImpl) Close() error {
	return r.db.Close()
}

// DBReady checks whether the database is reachable.
func (r *ProductRepoImpl) DBReady() bool {
	return r.db.Ping() == nil
}

// Migrate creates the products table if it does not exist.
func (r *ProductRepoImpl) Migrate(ctx context.Context) error {
	ddl := `CREATE TABLE IF NOT EXISTS products (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		sku_id VARCHAR(128) NOT NULL,
		title VARCHAR(512) NOT NULL,
		description TEXT NOT NULL,
		price VARCHAR(32) NOT NULL,
		currency VARCHAR(16) NOT NULL DEFAULT 'USDC',
		author VARCHAR(256) NOT NULL DEFAULT '',
		tags JSON NOT NULL,
		status VARCHAR(32) NOT NULL DEFAULT 'draft',
		skill_did VARCHAR(256) NOT NULL DEFAULT '',
		image_url VARCHAR(1024) NOT NULL DEFAULT '',
		created_at DATETIME(3) NOT NULL,
		updated_at DATETIME(3) NOT NULL,
		UNIQUE INDEX idx_sku_id (sku_id),
		INDEX idx_status (status)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`

	if _, err := r.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("mysql product repo: migrate: %w", err)
	}
	return nil
}

// FindAll returns paginated active products.
func (r *ProductRepoImpl) FindAll(ctx context.Context, page, size int) ([]*entity.Product, int64, error) {
	page, size = normalizePagination(page, size)
	offset := (page - 1) * size

	var total int64
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM products WHERE status = ?`, entity.ProductStatusActive,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("mysql product repo: count: %w", err)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, sku_id, title, description, price, currency, author, tags, image_url, status, skill_did, created_at, updated_at
		 FROM products WHERE status = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		entity.ProductStatusActive, size, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("mysql product repo: query: %w", err)
	}
	defer rows.Close()

	var products []*entity.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, 0, err
		}
		products = append(products, p)
	}
	return products, total, rows.Err()
}

// FindBySKUID returns a product by public SKU ID.
func (r *ProductRepoImpl) FindBySKUID(ctx context.Context, skuID string) (*entity.Product, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, sku_id, title, description, price, currency, author, tags, image_url, status, skill_did, created_at, updated_at
		 FROM products WHERE sku_id = ?`, strings.TrimSpace(skuID))
	product, err := scanProduct(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: %s", entity.ErrProductNotFound, skuID)
	}
	if err != nil {
		return nil, fmt.Errorf("mysql product repo: find by sku_id: %w", err)
	}
	return product, nil
}

// FindByID returns a product by internal ID.
func (r *ProductRepoImpl) FindByID(ctx context.Context, id int64) (*entity.Product, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, sku_id, title, description, price, currency, author, tags, image_url, status, skill_did, created_at, updated_at
		 FROM products WHERE id = ?`, id)
	product, err := scanProduct(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: id=%d", entity.ErrProductNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("mysql product repo: find by id: %w", err)
	}
	return product, nil
}

// Save inserts or updates a product.
func (r *ProductRepoImpl) Save(ctx context.Context, product *entity.Product) error {
	tagsJSON, err := json.Marshal(product.Tags)
	if err != nil {
		return fmt.Errorf("mysql product repo: marshal tags: %w", err)
	}

	if product.ID == 0 {
		now := time.Now()
		result, err := r.db.ExecContext(ctx,
			`INSERT INTO products (sku_id, title, description, price, currency, author, tags, status, skill_did, image_url, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			product.SKUID, product.Title, product.Description,
			product.Price, product.Currency, product.Author,
			string(tagsJSON), string(product.Status), product.SkillDid,
			product.ImageURL,
			now, now,
		)
		if err != nil {
			return fmt.Errorf("mysql product repo: insert: %w", err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		product.ID = id
		product.CreatedAt = now
		product.UpdatedAt = now
	} else {
		now := time.Now()
		_, err := r.db.ExecContext(ctx,
			`UPDATE products SET sku_id=?, title=?, description=?, price=?, currency=?, author=?, tags=?, status=?, skill_did=?, image_url=?, updated_at=?
			 WHERE id=?`,
			product.SKUID, product.Title, product.Description,
			product.Price, product.Currency, product.Author,
			string(tagsJSON), string(product.Status), product.SkillDid,
			product.ImageURL, now, product.ID,
		)
		if err != nil {
			return fmt.Errorf("mysql product repo: update: %w", err)
		}
		product.UpdatedAt = now
	}
	return nil
}

// UpdateStatus changes product lifecycle status.
func (r *ProductRepoImpl) UpdateStatus(ctx context.Context, id int64, status entity.ProductStatus) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE products SET status=?, updated_at=? WHERE id=?`,
		string(status), time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("mysql product repo: update status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("%w: id=%d", entity.ErrProductNotFound, id)
	}
	return nil
}

// Seed inserts sample products if the table is empty.
func (r *ProductRepoImpl) Seed(ctx context.Context, sellerAddress string) error {
	var count int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM products`).Scan(&count); err != nil {
		return fmt.Errorf("mysql product repo: seed count: %w", err)
	}
	if count > 0 {
		return nil
	}

	skillDid := entity.SkillDidPrefix + strings.TrimSpace(sellerAddress)
	now := time.Now()
	insertSQL := `INSERT INTO products (sku_id, title, description, price, currency, author, tags, status, skill_did, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'USDC', 'StablePay Research', ?, 'active', ?, ?, ?)`

	seeds := []struct {
		skuID, title, description string
		price                     string
		tags                      []string
	}{
		{"ai-agent-job-2025", "AI Agent 岗位分析报告 2025", "深入分析 2025 年 AI Agent 领域的岗位需求、技能要求、薪资水平和发展趋势", "2.00", []string{"AI", "Agent", "求职", "行业分析"}},
		{"industry-briefing-q1", "2025 Q1 行业研究简报", "涵盖 AI、区块链、Web3 领域的最新趋势和投资机会", "1.50", []string{"行业研究", "AI", "区块链", "Web3"}},
		{"resume-optimization-guide", "简历优化建议报告", "针对技术岗位的简历优化建议，包含模板和案例分析", "1.00", []string{"求职", "简历", "技术岗位"}},
		{"vitality-research", "生命力研究：为什么有些人看起来生命力很强", "基于萨特《恶心》的存在主义解读，探讨生命力的本质与来源", "1.50", []string{"哲学", "心理学", "存在主义", "个人成长"}},
	}

	for _, s := range seeds {
		tagsJSON, _ := json.Marshal(s.tags)
		if _, err := r.db.ExecContext(ctx, insertSQL, s.skuID, s.title, s.description, s.price, string(tagsJSON), skillDid, now, now); err != nil {
			return fmt.Errorf("mysql product repo: seed %s: %w", s.skuID, err)
		}
	}

	fmt.Printf("[mysql] seeded %d products\n", len(seeds))
	return nil
}

// ---------- helper types and functions ----------

// productScanner is satisfied by both *sql.Row and *sql.Rows.
type productScanner interface {
	Scan(dest ...any) error
}

func scanProduct(row productScanner) (*entity.Product, error) {
	var p entity.Product
	var tagsJSON, status, createdAt, updatedAt string
	if err := row.Scan(
		&p.ID,
		&p.SKUID,
		&p.Title,
		&p.Description,
		&p.Price,
		&p.Currency,
		&p.Author,
		&tagsJSON,
		&status,
		&p.SkillDid,
		&p.ImageURL,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}

	p.Status = entity.ProductStatus(status)
	if tagsJSON == "" {
		tagsJSON = "[]"
	}
	if err := json.Unmarshal([]byte(tagsJSON), &p.Tags); err != nil {
		return nil, fmt.Errorf("mysql product repo: unmarshal tags: %w", err)
	}

	var err error
	p.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		p.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, fmt.Errorf("mysql product repo: parse created_at: %w", err)
		}
	}
	p.UpdatedAt, err = time.Parse("2006-01-02 15:04:05", updatedAt)
	if err != nil {
		p.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("mysql product repo: parse updated_at: %w", err)
		}
	}
	return &p, nil
}

func normalizePagination(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return page, size
}
