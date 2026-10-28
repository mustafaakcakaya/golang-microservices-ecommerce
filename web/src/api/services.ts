import { api } from "./client";
import type { Card, Cart, CartItem, Checkout, Order, PaymentToken, Product } from "./types";

// Each service sits behind its own prefix on the gateway.
const catalog = "/catalog-service";
const basket = "/basket-service";
const ordering = "/ordering-service";
const payment = "/payment-service";

export const catalogApi = {
  products: (pageSize = 12) =>
    api
      .get<{ products: Product[] }>(`${catalog}/products?pageSize=${pageSize}`)
      .then((r) => r.products ?? []),

  product: (id: string) =>
    api.get<{ product: Product }>(`${catalog}/products/${id}`).then((r) => r.product),
};

export const basketApi = {
  /** An empty basket is a 404 rather than an empty object, which is the
   *  honest answer to "what is in this user's basket" when there is none. */
  get: (userName: string) =>
    api.get<{ cart: Cart }>(`${basket}/basket/${userName}`).then((r) => r.cart),

  store: (cart: { userName: string; items: CartItem[] }) =>
    api.post<{ userName: string }>(`${basket}/basket`, { cart }),

  remove: (userName: string) => api.delete<unknown>(`${basket}/basket/${userName}`),

  checkout: (checkout: Checkout) =>
    api.post<{ isSuccess: boolean }>(`${basket}/basket/checkout`, { checkout }),
};

export const orderingApi = {
  ofCustomer: (customerId: string) =>
    api
      .get<{ orders: Order[] }>(`${ordering}/orders/customer/${customerId}`)
      .then((r) => r.orders ?? []),
};

export const paymentApi = {
  /** Hands the card to the Payment service, the only one that keeps it, and
   *  returns the token every other call uses instead. */
  tokenize: (card: Card) => api.post<PaymentToken>(`${payment}/payment/tokens`, card),
};
