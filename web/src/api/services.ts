import { api } from "./client";
import type { Cart, CartItem, Checkout, Order, Product } from "./types";

// Each service sits behind its own prefix on the gateway.
const catalog = "/catalog-service";
const basket = "/basket-service";
const ordering = "/ordering-service";

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
