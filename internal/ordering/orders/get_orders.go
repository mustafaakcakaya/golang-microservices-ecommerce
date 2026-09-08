package orders

import (
	"context"
	"net/http"
	"strconv"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

// GetOrdersQuery pages through the orders. PageIndex is zero-based.
type GetOrdersQuery struct {
	Page pagination.Request
}

// GetOrdersResult is one page plus the total count, so a client can render
// "page 2 of 9" without a second call.
type GetOrdersResult struct {
	Orders pagination.Result[OrderView]
}

// GetOrdersHandler serves the query from the repository.
type GetOrdersHandler struct {
	repo Repository
}

// NewGetOrdersHandler wires the handler.
func NewGetOrdersHandler(repo Repository) *GetOrdersHandler {
	return &GetOrdersHandler{repo: repo}
}

// Handle implements cqrs.Handler.
func (h *GetOrdersHandler) Handle(ctx context.Context, q GetOrdersQuery) (GetOrdersResult, error) {
	list, total, err := h.repo.List(ctx, q.Page)
	if err != nil {
		return GetOrdersResult{}, err
	}

	return GetOrdersResult{
		Orders: pagination.NewResult(q.Page, total, viewsOf(list)),
	}, nil
}

// getOrdersResponse is the wire shape of a paged list.
type getOrdersResponse struct {
	Orders pagination.Result[OrderView] `json:"orders"`
}

// GetOrdersRoute adapts the handler to GET /orders?pageIndex=&pageSize=.
func GetOrdersRoute(handle cqrs.HandlerFunc[GetOrdersQuery, GetOrdersResult], log routeLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := GetOrdersQuery{
			Page: pagination.Request{
				PageIndex: intQuery(r, "pageIndex", 0),
				PageSize:  intQuery(r, "pageSize", pagination.DefaultPageSize),
			},
		}

		result, err := handle(r.Context(), query)
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusOK, getOrdersResponse(result))
	}
}

// intQuery reads an integer query parameter, falling back to def when absent
// or malformed. Paging values are clamped rather than rejected downstream, so a
// nonsense page is not worth a 400.
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
