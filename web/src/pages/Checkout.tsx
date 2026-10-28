import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { basketApi, paymentApi } from "../api/services";
import type { Card } from "../api/types";
import { Failed } from "../components";
import { shopper } from "../config";

/** Where the order goes. */
type Address = {
  firstName: string;
  lastName: string;
  emailAddress: string;
  addressLine: string;
  country: string;
  state: string;
  zipCode: string;
};

// The only method the shop takes for now.
const cardPaymentMethod = 1;

/**
 * Where the order goes and how it is paid.
 *
 * The card is sent to the Payment service, which keeps it and answers with a
 * token. Only the token and the last four digits go on to Basket, so the card
 * never reaches the basket, the order or any message between them. The card
 * fields are cleared as soon as the token is back.
 */
export function Checkout() {
  const navigate = useNavigate();
  const [submitting, setSubmitting] = useState(false);
  const [failure, setFailure] = useState<string>();
  const [address, setAddress] = useState<Address>({
    firstName: "",
    lastName: "",
    emailAddress: "",
    addressLine: "",
    country: "",
    state: "",
    zipCode: "",
  });
  const [card, setCard] = useState<Card>({
    cardName: "",
    cardNumber: "",
    expiration: "",
    cvv: "",
  });

  function addressField(name: keyof Address) {
    return {
      id: name,
      value: address[name],
      onChange: (event: React.ChangeEvent<HTMLInputElement>) =>
        setAddress((previous) => ({ ...previous, [name]: event.target.value })),
    };
  }

  function cardField(name: keyof Card) {
    return {
      id: name,
      value: card[name],
      // Never offered to the browser's form history.
      autoComplete: "off",
      onChange: (event: React.ChangeEvent<HTMLInputElement>) =>
        setCard((previous) => ({ ...previous, [name]: event.target.value })),
    };
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setFailure(undefined);

    try {
      const { paymentToken, cardLast4 } = await paymentApi.tokenize(card);
      setCard({ cardName: "", cardNumber: "", expiration: "", cvv: "" });

      await basketApi.checkout({
        userName: shopper.userName,
        customerId: shopper.customerId,
        ...address,
        paymentToken,
        cardLast4,
        paymentMethod: cardPaymentMethod,
      });
      navigate("/confirmation");
    } catch (error: unknown) {
      setFailure(error instanceof Error ? error.message : String(error));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <>
      <h2>Checkout</h2>
      <form onSubmit={(event) => void submit(event)} className="form">
        <label htmlFor="firstName">First name</label>
        <input {...addressField("firstName")} />

        <label htmlFor="lastName">Last name</label>
        <input {...addressField("lastName")} />

        <label htmlFor="emailAddress">Email</label>
        <input type="email" required {...addressField("emailAddress")} />

        <label htmlFor="addressLine">Address</label>
        <input required {...addressField("addressLine")} />

        <label htmlFor="country">Country</label>
        <input {...addressField("country")} />

        <label htmlFor="state">State</label>
        <input {...addressField("state")} />

        <label htmlFor="zipCode">Postcode</label>
        <input {...addressField("zipCode")} />

        <label htmlFor="cardName">Name on card</label>
        <input required {...cardField("cardName")} />

        <label htmlFor="cardNumber">Card number</label>
        <input required inputMode="numeric" {...cardField("cardNumber")} />

        <label htmlFor="expiration">Expiry (MM/YY)</label>
        <input required placeholder="MM/YY" {...cardField("expiration")} />

        <label htmlFor="cvv">Security code</label>
        <input required inputMode="numeric" maxLength={4} {...cardField("cvv")} />

        <button type="submit" disabled={submitting}>
          {submitting ? "Placing the order…" : "Place the order"}
        </button>
      </form>

      <p className="muted">
        Your card goes to the payment service only. The shop keeps a token and the last four digits.
      </p>

      {failure ? <Failed error={failure} /> : null}
    </>
  );
}
