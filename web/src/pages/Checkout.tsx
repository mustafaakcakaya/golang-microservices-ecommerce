import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { basketApi } from "../api/services";
import type { Checkout as CheckoutModel } from "../api/types";
import { Failed } from "../components";
import { shopper } from "../config";

/**
 * Where the order goes, and nothing else.
 *
 * There is deliberately no card number or security code on this form. The
 * message Basket publishes carries none, and the order it produces starts out
 * unpaid, so asking for card details here would mean collecting them for no
 * purpose - which is worse than not having them at all.
 */
export function Checkout() {
  const navigate = useNavigate();
  const [submitting, setSubmitting] = useState(false);
  const [failure, setFailure] = useState<string>();
  const [form, setForm] = useState<CheckoutModel>({
    userName: shopper.userName,
    customerId: shopper.customerId,
    firstName: "",
    lastName: "",
    emailAddress: "",
    addressLine: "",
    country: "",
    state: "",
    zipCode: "",
  });

  function field(name: keyof CheckoutModel) {
    return {
      id: name,
      value: form[name],
      onChange: (event: React.ChangeEvent<HTMLInputElement>) =>
        setForm((previous) => ({ ...previous, [name]: event.target.value })),
    };
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setFailure(undefined);

    try {
      await basketApi.checkout(form);
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
        <input {...field("firstName")} />

        <label htmlFor="lastName">Last name</label>
        <input {...field("lastName")} />

        <label htmlFor="emailAddress">Email</label>
        <input type="email" required {...field("emailAddress")} />

        <label htmlFor="addressLine">Address</label>
        <input required {...field("addressLine")} />

        <label htmlFor="country">Country</label>
        <input {...field("country")} />

        <label htmlFor="state">State</label>
        <input {...field("state")} />

        <label htmlFor="zipCode">Postcode</label>
        <input {...field("zipCode")} />

        <button type="submit" disabled={submitting}>
          {submitting ? "Placing the order…" : "Place the order"}
        </button>
      </form>

      <p className="muted">
        Payment is collected separately. The order is placed as a draft until it is paid for.
      </p>

      {failure ? <Failed error={failure} /> : null}
    </>
  );
}
