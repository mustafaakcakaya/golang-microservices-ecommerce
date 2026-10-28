package cards

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/cqrs"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/httpx"
)

// errCardSerialized is returned rather than a redacted object; see
// TokenizeCardCommand.MarshalJSON.
var errCardSerialized = errors.New("card details must not be serialized")

// TokenizeCardCommand hands a card to this service in exchange for a token.
//
// Both the fmt and the encoding/json representations refuse to render the
// card, which is what keeps a %v of a command out of the logs and a stray
// marshal out of a response body.
type TokenizeCardCommand struct {
	CardName   string `json:"cardName" validate:"required"`
	CardNumber string `json:"cardNumber" validate:"required"`
	Expiration string `json:"expiration" validate:"required"`
	CVV        string `json:"cvv" validate:"required"`
}

// String redacts the card so formatting a command cannot leak it.
func (c TokenizeCardCommand) String() string {
	return "TokenizeCardCommand{card: [redacted]}"
}

// MarshalJSON refuses to serialize the card.
func (c TokenizeCardCommand) MarshalJSON() ([]byte, error) {
	return nil, errCardSerialized
}

// TokenizeCardResult is what other services may know about the card.
type TokenizeCardResult struct {
	PaymentToken string
	CardLast4    string
}

// TokenizeCardHandler checks a card, keeps it and issues its token.
type TokenizeCardHandler struct {
	repo Repository
	now  func() time.Time
}

// NewTokenizeCardHandler wires the handler. now is the clock expiry is judged
// against; nil means the system clock.
func NewTokenizeCardHandler(repo Repository, now func() time.Time) *TokenizeCardHandler {
	if now == nil {
		now = time.Now
	}

	return &TokenizeCardHandler{repo: repo, now: now}
}

// Handle implements cqrs.Handler.
func (h *TokenizeCardHandler) Handle(ctx context.Context, c TokenizeCardCommand) (TokenizeCardResult, error) {
	number := normalizeCardNumber(c.CardNumber)

	if err := h.check(c, number); err != nil {
		return TokenizeCardResult{}, err
	}

	// The security code has done its job by now and goes no further: it is
	// not part of what is stored, so it is not passed on.
	card := StoredCard{
		Token:      newToken(),
		CardNumber: number,
		Expiration: c.Expiration,
		CardName:   strings.TrimSpace(c.CardName),
	}

	if err := h.repo.Store(ctx, card); err != nil {
		return TokenizeCardResult{}, err
	}

	return TokenizeCardResult{PaymentToken: card.Token, CardLast4: card.Last4()}, nil
}

// check applies the card rules.
//
// The messages say what is wrong without repeating what was sent. They are
// returned to the caller and logged on the way, and a message that quoted the
// value would put the card number in both places.
func (h *TokenizeCardHandler) check(c TokenizeCardCommand, number string) error {
	fields := map[string]string{}

	if !validCardNumber(number) {
		fields["CardNumber"] = "CardNumber is not a valid card number"
	}

	if end, ok := expiryEnd(c.Expiration); !ok {
		fields["Expiration"] = "Expiration must be MM/YY"
	} else if !h.now().Before(end) {
		fields["Expiration"] = "the card has expired"
	}

	if !validCVV(c.CVV) {
		fields["CVV"] = "CVV must be 3 or 4 digits"
	}

	if len(fields) > 0 {
		return apperr.Validation(fields)
	}

	return nil
}

// tokenizeCardResponse is the wire shape of a tokenize result.
type tokenizeCardResponse struct {
	PaymentToken string `json:"paymentToken"`
	CardLast4    string `json:"cardLast4"`
}

// TokenizeCardRoute adapts the handler to POST /payment/tokens.
func TokenizeCardRoute(
	handle cqrs.HandlerFunc[TokenizeCardCommand, TokenizeCardResult], log routeLogger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body TokenizeCardCommand
		if err := httpx.DecodeJSON(w, r, &body); err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		result, err := handle(r.Context(), body)
		if err != nil {
			httpx.Error(r.Context(), w, log, err)
			return
		}

		httpx.JSON(w, http.StatusCreated, tokenizeCardResponse(result))
	}
}
