export type Product = {
  id: string;
  name: string;
  category: string[];
  description: string;
  imageFile: string;
  price: number;
};

export type CartItem = {
  quantity: number;
  color: string;
  price: number;
  productId: string;
  productName: string;
};

export type Cart = {
  userName: string;
  items: CartItem[];
  totalPrice: number;
};

export type OrderItem = {
  orderId: string;
  productId: string;
  quantity: number;
  price: number;
};

/** How an order was paid, as far as anyone but the Payment service may know. */
export type Payment = {
  cardLast4: string;
  paymentMethod: number;
};

export type Order = {
  id: string;
  customerId: string;
  status: string;
  shippingAddress: Address;
  billingAddress: Address;
  // Absent for orders stored without a payment.
  payment: Payment | null;
  orderItems: OrderItem[];
  totalPrice: number;
};

export type Address = {
  firstName: string;
  lastName: string;
  emailAddress: string;
  addressLine: string;
  country: string;
  state: string;
  zipCode: string;
};

/** What Basket is told at checkout. No card: only the token the Payment
 *  service issued for it, and the last four digits to show the customer. */
export type Checkout = {
  userName: string;
  customerId: string;
  firstName: string;
  lastName: string;
  emailAddress: string;
  addressLine: string;
  country: string;
  state: string;
  zipCode: string;
  paymentToken: string;
  cardLast4: string;
  paymentMethod: number;
};

/** The card, which goes to the Payment service and nowhere else. */
export type Card = {
  cardName: string;
  cardNumber: string;
  expiration: string;
  cvv: string;
};

export type PaymentToken = {
  paymentToken: string;
  cardLast4: string;
};
