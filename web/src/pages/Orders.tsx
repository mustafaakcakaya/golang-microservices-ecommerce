import { orderingApi } from "../api/services";
import { Empty, Failed, Loading, price } from "../components";
import { shopper } from "../config";
import { useAsync } from "../useAsync";

export function Orders() {
  const {
    data: orders,
    error,
    loading,
    reload,
  } = useAsync(() => orderingApi.ofCustomer(shopper.customerId));

  if (loading) {
    return <Loading />;
  }
  if (error) {
    return <Failed error={error} onRetry={reload} />;
  }
  if (!orders?.length) {
    return (
      <>
        <h2>Your orders</h2>
        <Empty>No orders yet.</Empty>
      </>
    );
  }

  return (
    <>
      <h2>Your orders</h2>
      <button type="button" onClick={reload}>
        Refresh
      </button>
      <table>
        <thead>
          <tr>
            <th>Reference</th>
            <th>Status</th>
            <th>Items</th>
            <th>Payment</th>
            <th>Total</th>
          </tr>
        </thead>
        <tbody>
          {orders.map((order) => (
            <tr key={order.id}>
              <td>{order.orderName}</td>
              <td>{order.status}</td>
              <td>{order.orderItems.length}</td>
              {/* Only the last four digits ever reach the storefront. Orders
                  stored without a payment come back as null. */}
              <td>{order.payment ? `•••• ${order.payment.cardLast4}` : "No payment"}</td>
              <td className="price">{price(order.totalPrice)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  );
}
