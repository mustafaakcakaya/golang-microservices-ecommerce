package pagination_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/pagination"
)

func TestNormalizedAppliesDefaultsAndClamps(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   pagination.Request
		want pagination.Request
	}{
		{"zero value gets defaults", pagination.Request{}, pagination.Request{PageIndex: 0, PageSize: pagination.DefaultPageSize}},
		{"negative page index clamps to 0", pagination.Request{PageIndex: -3, PageSize: 5}, pagination.Request{PageIndex: 0, PageSize: 5}},
		{"negative page size gets default", pagination.Request{PageIndex: 2, PageSize: -1}, pagination.Request{PageIndex: 2, PageSize: pagination.DefaultPageSize}},
		{"oversized page clamps to max", pagination.Request{PageIndex: 1, PageSize: 10_000}, pagination.Request{PageIndex: 1, PageSize: pagination.MaxPageSize}},
		{"valid input is untouched", pagination.Request{PageIndex: 3, PageSize: 25}, pagination.Request{PageIndex: 3, PageSize: 25}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.in.Normalized(); got != tc.want {
				t.Errorf("Normalized() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestOffsetAndLimitDeriveFromNormalizedValues(t *testing.T) {
	t.Parallel()

	req := pagination.Request{PageIndex: 3, PageSize: 20}

	if got := req.Offset(); got != 60 {
		t.Errorf("Offset() = %d, want 60", got)
	}
	if got := req.Limit(); got != 20 {
		t.Errorf("Limit() = %d, want 20", got)
	}

	// A negative page must not produce a negative SQL OFFSET.
	if got := (pagination.Request{PageIndex: -1, PageSize: 20}).Offset(); got != 0 {
		t.Errorf("Offset() for negative page = %d, want 0", got)
	}
}

func TestNewResultSerializesEmptyPageAsArray(t *testing.T) {
	t.Parallel()

	result := pagination.NewResult[string](pagination.Request{}, 0, nil)

	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Clients iterate over data; null would force every consumer to nil-check.
	if !strings.Contains(string(body), `"data":[]`) {
		t.Errorf("body = %s, want data serialized as []", body)
	}
}

func TestNewResultReportsNormalizedPaging(t *testing.T) {
	t.Parallel()

	result := pagination.NewResult(pagination.Request{PageIndex: -5, PageSize: 0}, 42, []int{1, 2, 3})

	if result.PageIndex != 0 || result.PageSize != pagination.DefaultPageSize {
		t.Errorf("paging = (%d, %d), want normalized (0, %d)", result.PageIndex, result.PageSize, pagination.DefaultPageSize)
	}
	if result.Count != 42 {
		t.Errorf("count = %d, want 42", result.Count)
	}
	if len(result.Data) != 3 {
		t.Errorf("data length = %d, want 3", len(result.Data))
	}
}
