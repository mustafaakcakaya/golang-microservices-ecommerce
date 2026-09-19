// The one address the storefront knows. Everything goes through the gateway:
// the services are not reachable from a browser, and their own ports exist for
// local work rather than for clients.
export const apiBase = import.meta.env.VITE_API_BASE ?? "/api";

// There is no sign-in yet, so the storefront shops as one fixed customer. The
// id has to be a customer Ordering knows, since an order refers to one.
export const shopper = {
  userName: import.meta.env.VITE_SHOPPER_USERNAME ?? "swn01",
  customerId:
    import.meta.env.VITE_SHOPPER_CUSTOMER_ID ?? "58c49479-ec65-4de2-86e7-033c546291aa",
};
