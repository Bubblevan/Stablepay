// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package sqlite implements the ProductRepository port for SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/stablepay/merchant-server/internal/domain/entity"
)

const defaultDBPath = "./data/merchant.db"

// ProductRepoImpl implements repository.ProductRepository backed by SQLite.
type ProductRepoImpl struct {
	db   *sql.DB
	path string
	mu   sync.RWMutex
}

// NewProductRepo opens (or creates) a SQLite database and runs optional migrations.
func NewProductRepo(path string, autoMigrate bool) (*ProductRepoImpl, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultDBPath
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("sqlite product repo: create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite product repo: open database: %w", err)
	}

	// SQLite is embedded and file based. A single writer is easiest to reason
	// about for this MVP and avoids many "database is locked" surprises.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	repo := &ProductRepoImpl{db: db, path: path}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := repo.configure(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if autoMigrate {
		if err := repo.Migrate(ctx); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return repo, nil
}

func (r *ProductRepoImpl) configure(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := r.db.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("sqlite product repo: configure pragma %s: %w", p, err)
		}
	}
	return nil
}

// Migrate creates the products table if it does not exist.
func (r *ProductRepoImpl) Migrate(ctx context.Context) error {
	schema := `CREATE TABLE IF NOT EXISTS products (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sku_id TEXT NOT NULL UNIQUE,
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		price TEXT NOT NULL,
		currency TEXT NOT NULL DEFAULT 'USDC',
		author TEXT NOT NULL DEFAULT '',
		tags TEXT NOT NULL DEFAULT '[]',
		image_url TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'draft',
		skill_did TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_products_sku_id ON products(sku_id);
	CREATE INDEX IF NOT EXISTS idx_products_status ON products(status);`
	if _, err := r.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("sqlite product repo: migrate: %w", err)
	}
	return nil
}

// Close shuts down the database connection.
func (r *ProductRepoImpl) Close() error {
	return r.db.Close()
}

// FindAll returns paginated active products and total count.
func (r *ProductRepoImpl) FindAll(ctx context.Context, page, size int) ([]*entity.Product, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	page, size = normalizePagination(page, size)
	offset := (page - 1) * size

	var total int64
	countQuery := `SELECT COUNT(*) FROM products WHERE status = ?`
	if err := r.db.QueryRowContext(ctx, countQuery, entity.ProductStatusActive).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("sqlite product repo: count products: %w", err)
	}

	query := `SELECT id, sku_id, title, description, price, currency, author, tags, image_url, status, skill_did, created_at, updated_at
		FROM products WHERE status = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`
	rows, err := r.db.QueryContext(ctx, query, entity.ProductStatusActive, size, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("sqlite product repo: query products: %w", err)
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
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("sqlite product repo: iterate products: %w", err)
	}

	return products, total, nil
}

// FindBySKUID returns a product by its public SKU ID.
func (r *ProductRepoImpl) FindBySKUID(ctx context.Context, skuID string) (*entity.Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	query := `SELECT id, sku_id, title, description, price, currency, author, tags, image_url, status, skill_did, created_at, updated_at
		FROM products WHERE sku_id = ?`
	row := r.db.QueryRowContext(ctx, query, strings.TrimSpace(skuID))
	product, err := scanProduct(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: %s", entity.ErrProductNotFound, skuID)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite product repo: find by sku_id: %w", err)
	}
	return product, nil
}

// FindByID returns a product by its internal persistence ID.
func (r *ProductRepoImpl) FindByID(ctx context.Context, id int64) (*entity.Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	query := `SELECT id, sku_id, title, description, price, currency, author, tags, image_url, status, skill_did, created_at, updated_at
		FROM products WHERE id = ?`
	row := r.db.QueryRowContext(ctx, query, id)
	product, err := scanProduct(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: id=%d", entity.ErrProductNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite product repo: find by id: %w", err)
	}
	return product, nil
}

// Save inserts or updates a product.
func (r *ProductRepoImpl) Save(ctx context.Context, product *entity.Product) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tagsJSON, err := json.Marshal(product.Tags)
	if err != nil {
		return fmt.Errorf("sqlite product repo: marshal tags: %w", err)
	}

	if product.ID == 0 {
		// Insert
		query := `INSERT INTO products (sku_id, title, description, price, currency, author, tags, image_url, status, skill_did, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		now := formatTime(time.Now())
		result, err := r.db.ExecContext(ctx, query,
			product.SKUID, product.Title, product.Description,
			product.Price, product.Currency, product.Author,
			string(tagsJSON), product.ImageURL, string(product.Status), product.SkillDid,
			now, now,
		)
		if err != nil {
			return fmt.Errorf("sqlite product repo: insert product: %w", err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("sqlite product repo: get last insert id: %w", err)
		}
		product.ID = id
		product.CreatedAt, _ = parseTime(now)
		product.UpdatedAt = product.CreatedAt
	} else {
		// Update
		query := `UPDATE products SET sku_id=?, title=?, description=?, price=?, currency=?, author=?, tags=?, status=?, skill_did=?, updated_at=?
			WHERE id=?`
		now := formatTime(time.Now())
		_, err := r.db.ExecContext(ctx, query,
			product.SKUID, product.Title, product.Description,
			product.Price, product.Currency, product.Author,
			string(tagsJSON), string(product.Status), product.SkillDid,
			now, product.ID,
		)
		if err != nil {
			return fmt.Errorf("sqlite product repo: update product: %w", err)
		}
		product.UpdatedAt, _ = parseTime(now)
	}
	return nil
}

// UpdateStatus changes the lifecycle status of a product.
func (r *ProductRepoImpl) UpdateStatus(ctx context.Context, id int64, status entity.ProductStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	query := `UPDATE products SET status=?, updated_at=? WHERE id=?`
	now := formatTime(time.Now())
	result, err := r.db.ExecContext(ctx, query, string(status), now, id)
	if err != nil {
		return fmt.Errorf("sqlite product repo: update status: %w", err)
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
	r.mu.Lock()
	defer r.mu.Unlock()

	var count int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM products`).Scan(&count); err != nil {
		return fmt.Errorf("sqlite product repo: seed count: %w", err)
	}
	if count > 0 {
		return nil // already seeded
	}

	skillDid := entity.SkillDidPrefix + strings.TrimSpace(sellerAddress)
	now := formatTime(time.Now())

	seedProducts := []struct {
		skuID, title, description, price string
		tags                             []string
	}{
		{
			skuID: "ai-agent-job-2025", title: "AI Agent 岗位分析报告 2025",
			description: "深入分析 2025 年 AI Agent 领域的岗位需求、技能要求、薪资水平和发展趋势",
			price: "2.00", tags: []string{"AI", "Agent", "求职", "行业分析"},
		},
		{
			skuID: "industry-briefing-q1", title: "2025 Q1 行业研究简报",
			description: "涵盖 AI、区块链、Web3 领域的最新趋势和投资机会",
			price: "1.50", tags: []string{"行业研究", "AI", "区块链", "Web3"},
		},
		{
			skuID: "resume-optimization-guide", title: "简历优化建议报告",
			description: "针对技术岗位的简历优化建议，包含模板和案例分析",
			price: "1.00", tags: []string{"求职", "简历", "技术岗位"},
		},
		{
			skuID: "vitality-research", title: "生命力研究：为什么有些人看起来生命力很强",
			description: "基于萨特《恶心》的存在主义解读，探讨生命力的本质与来源",
			price: "1.50", tags: []string{"哲学", "心理学", "存在主义", "个人成长"},
		},
	}

	query := `INSERT INTO products (sku_id, title, description, price, currency, author, tags, status, skill_did, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'USDC', 'StablePay Research', ?, 'active', ?, ?, ?)`

	for _, sp := range seedProducts {
		tagsJSON, _ := json.Marshal(sp.tags)
		if _, err := r.db.ExecContext(ctx, query,
			sp.skuID, sp.title, sp.description, sp.price,
			string(tagsJSON), skillDid, now, now,
		); err != nil {
			return fmt.Errorf("sqlite product repo: seed product %s: %w", sp.skuID, err)
		}
	}

	fmt.Printf("[sqlite] seeded %d products\n", len(seedProducts))
	return nil
}

// DBReady checks whether the underlying database is reachable.
func (r *ProductRepoImpl) DBReady() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.db.Ping() == nil
}

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
		return nil, fmt.Errorf("sqlite product repo: unmarshal product tags: %w", err)
	}

	var err error
	p.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, fmt.Errorf("sqlite product repo: parse created_at: %w", err)
	}
	p.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return nil, fmt.Errorf("sqlite product repo: parse updated_at: %w", err)
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

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func parseTime(raw string) (time.Time, error) {
	return time.Parse(time.RFC3339, raw)
}
