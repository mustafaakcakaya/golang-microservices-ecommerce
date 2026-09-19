import { Link } from "react-router-dom";

import { ApiError } from "../api/client";
import { basketApi } from "../api/services";
import type { Cart as CartModel } from "../api/types";
import { Empty, Failed, Loading, price } from "../components";
import { shopper } from "../config";
import { useAsync } from "../useAsync";

export function Cart() {
  const { data: cart, error, loading, reload } = useAsync(() => loadCart());

  if (loading) {
    return <Loading />;
  }
  if (error) {
    return <Failed error={error} onRetry={reload} />;
  }
  if (!cart || cart.items.length === 0) {
    return (
      <>
        <h2>Your cart</h2>
        <Empty>
          Nothing here yet. <Link to="/products">Browse the catalog</Link>.
        </Empty>
      </>
    );
  }

  return (
    <>
      <h2>Your cart</h2>
      <table>
        <thead>
          <tr>
            <th>Product</th>
            <th>Quantity</th>
            <th>Price</th>
            <th>Line total</th>
          </tr>
        </thead>
        <tbody>
          {cart.items.map((item) => (
            <tr key={item.productId}>
              <td>{item.productName}</td>
              <td>{item.quantity}</td>
              {/* The price already has any discount applied: the basket
                  stores what the customer will be charged. */}
              <td>{price(item.price)}</td>
              <td>{price(item.price * item.quantity)}</td>
            </tr>
          ))}
        </tbody>
        <tfoot>
          <tr>
            <td colSpan={3}>Total</td>
            <td className="price">{price(cart.totalPrice)}</td>
          </tr>
        </tfoot>
      </table>

      <div className="row">
        <Link to="/checkout" className="button">
          Checkout
        </Link>
        <button type="button" onClick={() => void clear(reload)}>
          Empty the cart
        </button>
      </div>
    </>
  );
}

/** loadCart treats "no basket yet" as an empty one rather than a failure. */
async function loadCart(): Promise<CartModel | undefined> {
  try {
    return await basketApi.get(shopper.userName);
  } catch (error: unknown) {
    if (error instanceof ApiError && error.status === 404) {
      return undefined;
    }
    throw error;
  }
}

async function clear(reload: () => void) {
  await basketApi.remove(shopper.userName);
  reload();
}
