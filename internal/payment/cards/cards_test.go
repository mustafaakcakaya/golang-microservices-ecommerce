package cards_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/payment/cards"
)

const (
	cardNumber = "5555555555554444"
	// Letters no UUID, timestamp or token can contain, so a check for the
	// security code in logs or responses cannot match it by chance. The
	// rules want digits, so it is only used where it is expected to fail.
	unlikelyCVV = "zqx"
	cvv         = "737"
)

// fakeRepository keeps cards in memory.
type fakeRepository struct {
	mu    sync.Mutex
	cards []cards.StoredCard
}

func (r *fakeRepository) Store(_ context.Context, card cards.StoredCard) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cards = append(r.cards, card)
	return nil
}

// fixedNow is a clock where 12/28 has not expired and 01/20 has.
func fixedNow() time.Time {
	return time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)
}

func newRouter(repo cards.Repository, logs *bytes.Buffer) http.Handler {
	r := chi.NewRouter()
	cards.RegisterRoutes(r, repo, fixedNow, slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return r
}

func tokenize(t *testing.T, router http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/payment/tokens", bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")

	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	return response
}

func validCard() map[string]any {
	return map[string]any{
		"cardName":   "Mustafa Akcakaya",
		"cardNumber": "5555 5555 5555 4444",
		"expiration": "12/28",
		"cvv":        cvv,
	}
}

func TestACardIsExchangedForATokenAndItsLastFourDigits(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	var logs bytes.Buffer

	response := tokenize(t, newRouter(repo, &logs), validCard())

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", response.Code, response.Body)
	}

	var body struct {
		PaymentToken string `json:"paymentToken"`
		CardLast4    string `json:"cardLast4"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if !strings.HasPrefix(body.PaymentToken, "pt_") || body.CardLast4 != "4444" {
		t.Errorf("response = %+v, want a pt_ token and 4444", body)
	}

	// The response is what the storefront passes on; the card stops here.
	if strings.Contains(response.Body.String(), cardNumber) || strings.Contains(response.Body.String(), cvv) {
		t.Errorf("the response repeats the card: %s", response.Body)
	}

	if len(repo.cards) != 1 {
		t.Fatalf("stored %d cards, want one", len(repo.cards))
	}
	stored := repo.cards[0]
	if stored.Token != body.PaymentToken || stored.CardNumber != cardNumber ||
		stored.Expiration != "12/28" || stored.CardName != "Mustafa Akcakaya" {
		t.Errorf("stored = %+v, want the card under the issued token", stored)
	}
}

func TestTheSecurityCodeIsNeverKept(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	var logs bytes.Buffer

	if response := tokenize(t, newRouter(repo, &logs), validCard()); response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", response.Code, response.Body)
	}

	// Checked over everything stored rather than one field, so a field added
	// later cannot carry it either.
	if stored := fmt.Sprintf("%#v", repo.cards); strings.Contains(stored, cvv) {
		t.Errorf("the security code was stored: %s", stored)
	}
	if strings.Contains(logs.String(), cvv) || strings.Contains(logs.String(), cardNumber) {
		t.Errorf("the card reached the logs: %s", logs.String())
	}
}

func TestABadCardIsRefusedWithoutRepeatingIt(t *testing.T) {
	t.Parallel()

	for name, change := range map[string]func(map[string]any){
		"failed check digit": func(card map[string]any) { card["cardNumber"] = "5555555555554445" },
		"expired":            func(card map[string]any) { card["expiration"] = "01/20" },
		"not an expiry":      func(card map[string]any) { card["expiration"] = "2028-12" },
		"security code":      func(card map[string]any) { card["cvv"] = unlikelyCVV },
		"no name":            func(card map[string]any) { card["cardName"] = "" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeRepository{}
			var logs bytes.Buffer

			card := validCard()
			change(card)

			response := tokenize(t, newRouter(repo, &logs), card)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", response.Code, response.Body)
			}
			if len(repo.cards) != 0 {
				t.Error("a card that failed its checks was stored")
			}

			// What was sent must not come back, nor end up in the logs.
			for _, value := range []string{"5555555555554445", cardNumber, unlikelyCVV, "2028-12"} {
				if strings.Contains(response.Body.String(), value) {
					t.Errorf("the response repeats %q: %s", value, response.Body)
				}
				if strings.Contains(logs.String(), value) {
					t.Errorf("the logs repeat %q: %s", value, logs.String())
				}
			}
		})
	}
}

func TestACommandNeverRendersTheCard(t *testing.T) {
	t.Parallel()

	command := cards.TokenizeCardCommand{
		CardName: "Mustafa Akcakaya", CardNumber: cardNumber, Expiration: "12/28", CVV: unlikelyCVV,
	}

	for _, rendered := range []string{fmt.Sprintf("%v", command), fmt.Sprintf("%+v", command)} {
		if strings.Contains(rendered, cardNumber) || strings.Contains(rendered, unlikelyCVV) {
			t.Errorf("a rendered command leaked the card: %s", rendered)
		}
	}
	if _, err := json.Marshal(command); err == nil {
		t.Error("a command carrying a card must refuse to serialize")
	}

	stored := cards.StoredCard{Token: "pt_x", CardNumber: cardNumber}
	if rendered := fmt.Sprintf("%v", stored); strings.Contains(rendered, cardNumber) {
		t.Errorf("a rendered stored card leaked the number: %s", rendered)
	}
}
