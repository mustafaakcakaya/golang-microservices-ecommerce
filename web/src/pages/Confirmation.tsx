import { Link } from "react-router-dom";

export function Confirmation() {
  return (
    <section className="detail">
      <h2>Thank you</h2>
      {/* Honest about what just happened: the checkout was accepted, and the
          order is created by another service reacting to it. */}
      <p>
        Your checkout has been accepted. The order is being created and will appear in your
        order history shortly.
      </p>
      <Link to="/orders" className="button">
        See your orders
      </Link>
    </section>
  );
}
