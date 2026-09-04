// Package pagination carries paging input and results between layers.
package pagination

// DefaultPageSize matches the .NET PaginationRequest default.
const DefaultPageSize = 10

// MaxPageSize caps what a caller may request, so a single request cannot pull
// the whole table. The .NET version has no such cap; it is added here because
// the limit reaches SQL directly.
const MaxPageSize = 100

// Request is the paging input, zero-based like the .NET PaginationRequest.
type Request struct {
	PageIndex int `json:"pageIndex"`
	PageSize  int `json:"pageSize"`
}

// Normalized clamps out-of-range input instead of rejecting it: paging values
// are a display concern, and a negative page is not worth a 400.
func (r Request) Normalized() Request {
	if r.PageIndex < 0 {
		r.PageIndex = 0
	}
	switch {
	case r.PageSize <= 0:
		r.PageSize = DefaultPageSize
	case r.PageSize > MaxPageSize:
		r.PageSize = MaxPageSize
	}
	return r
}

// Offset is the number of rows to skip for this page.
func (r Request) Offset() int {
	n := r.Normalized()
	return n.PageIndex * n.PageSize
}

// Limit is the number of rows to read for this page.
func (r Request) Limit() int {
	return r.Normalized().PageSize
}

// Result is one page plus the total row count.
type Result[T any] struct {
	PageIndex int   `json:"pageIndex"`
	PageSize  int   `json:"pageSize"`
	Count     int64 `json:"count"`
	Data      []T   `json:"data"`
}

// NewResult builds a page, guaranteeing Data serializes as [] rather than null
// when the page is empty.
func NewResult[T any](request Request, count int64, data []T) Result[T] {
	normalized := request.Normalized()

	if data == nil {
		data = []T{}
	}

	return Result[T]{
		PageIndex: normalized.PageIndex,
		PageSize:  normalized.PageSize,
		Count:     count,
		Data:      data,
	}
}
