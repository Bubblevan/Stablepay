package main

import (
	"context"
	query_service "query-service/kitex_gen/stablepay/query_service"
)

// QueryServiceImpl implements the last service interface defined in the IDL.
type QueryServiceImpl struct{}

// GetBalanceSummary implements the QueryServiceImpl interface.
func (s *QueryServiceImpl) GetBalanceSummary(ctx context.Context, req *query_service.GetBalanceSummaryRequest) (resp *query_service.GetBalanceSummaryResponse, err error) {
	// TODO: Your code here...
	return
}

// ListTransactions implements the QueryServiceImpl interface.
func (s *QueryServiceImpl) ListTransactions(ctx context.Context, req *query_service.ListTransactionsRequest) (resp *query_service.ListTransactionsResponse, err error) {
	// TODO: Your code here...
	return
}

// GetRevenueSummary implements the QueryServiceImpl interface.
func (s *QueryServiceImpl) GetRevenueSummary(ctx context.Context, req *query_service.GetRevenueSummaryRequest) (resp *query_service.GetRevenueSummaryResponse, err error) {
	// TODO: Your code here...
	return
}
