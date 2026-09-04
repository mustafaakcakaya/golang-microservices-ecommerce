package products

import (
	"context"
	"net/http"
	"strconv"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

// GetProductsQuery pages through the catalog. PageNumber is 1-based on the
// wire, as in the .NET endpoint; it is converted to the zero-based
// pagination.Request internally.
type GetProductsQuery struct {
	PageNumber int
	PageSize   int
}

// GetProductsResult is the handler output.
type GetProductsResult struct {
	Products []Product
}

// GetProductsHandler serves the query from the repository.
type GetProductsHandler struct {
	repo Repository
}

// NewGetProductsHandler wires the handler.
func NewGetProductsHandler(repo Repository) *GetProductsHandler {
	return &GetProductsHandler{repo: repo}
}

// Handle implements cqrs.Handler.
func (h *GetProductsHandler) Handle(ctx context.Context, q GetProductsQuery) (GetProductsResult, error) {
	page := pagination.Request{PageIndex: q.PageNumber - 1, PageSize: q.PageSize}

	// The count is part of the repository contract for paged results; this
	// endpoint, like its .NET counterpart, exposes only the items.
	items, _, err := h.repo.List(ctx, page)
	if err != nil {
		return GetProductsResult{}, err
	}

	return GetProductsResult{Products: items}, nil
}

// getProductsResponse mirrors the .NET GetProductsResponse shape.
type getProductsResponse struct {
	Products []Product `json:"products"`
}

// GetProductsRoute adapts the handler to GET /products?pageNumber=&pageSize=.
func GetProductsRoute(handle cqrs.HandlerFunc[GetProductsQuery, GetProductsResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := GetProductsQuery{
			PageNumber: intQuery(r, "pageNumber", 1),
			PageSize:   intQuery(r, "pageSize", pagination.DefaultPageSize),
		}

		result, err := handle(r.Context(), query)
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, getProductsResponse(result))
	}
}

// intQuery reads an integer query parameter, falling back to def when absent
// or malformed; paging values are display hints, not worth a 400.
func intQuery(r *http.Request, name string, def int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return value
}
