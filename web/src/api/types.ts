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

export type Payment = {
  cardName: string;
  maskedCardNumber: string;
  expiration: string;
  paymentMethod: number;
};

export type Order = {
  id: string;
  customerId: string;
  orderName: string;
  status: string;
  shippingAddress: Address;
  billingAddress: Address;
  // Absent until the order is paid for: a checkout carries no card details.
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

/** What a checkout asks for. No card details: the message that leaves Basket
 *  carries none, so collecting them here would serve no purpose. */
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
};
