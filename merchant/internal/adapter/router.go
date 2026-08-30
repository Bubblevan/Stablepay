// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package adapter implements the COLA-style Adapter layer.
//
// Adapter responsibilities:
// - HTTP routing
// - request/path/query/header parsing
// - response JSON and headers
// - middleware hooks for logging/auth/recovery/rate limiting
//
// Adapter calls Application Services and should not contain product payment
// business rules.
package adapter

import (
	"github.com/cloudwego/hertz/pkg/app/server"

	"github.com/stablepay/merchant-server/internal/adapter/handler"
)

// Router registers all HTTP routes.
type Router struct {
	srv            *server.Hertz
	healthHandler  *handler.HealthHandler
	productHandler *handler.ProductHandler
}

// NewRouter creates a route registrar.
func NewRouter(srv *server.Hertz, hh *handler.HealthHandler, ph *handler.ProductHandler) *Router {
	return &Router{
		srv:            srv,
		healthHandler:  hh,
		productHandler: ph,
	}
}

// Register registers operations and public API routes.
func (r *Router) Register() {
	// Operations endpoints. These stay outside /api/v1 so Kubernetes probes and
	// monitoring tools do not need business API knowledge.
	r.srv.GET("/healthz", r.healthHandler.Healthz)
	r.srv.GET("/merchant/healthz", r.healthHandler.Healthz)

	// Public Merchant API. /merchant is the ACK Ingress prefix.
	r.registerV1("/api/v1")
	r.registerV1("/merchant/api/v1")
}

func (r *Router) registerV1(prefix string) {
	v1 := r.srv.Group(prefix)
	{
		v1.GET("/products", r.productHandler.ListProducts)
		v1.GET("/products/:id", r.productHandler.GetProduct)
		v1.GET("/products/:id/execute", r.productHandler.ExecutePurchase)
	}
}
