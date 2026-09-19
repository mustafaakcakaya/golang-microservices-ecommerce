import type { ReactNode } from "react";
import { Link } from "react-router-dom";

import type { Product } from "./api/types";

/** Money as the services report it: a number, formatted once, here. */
export function price(amount: number): string {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
  }).format(amount);
}

export function Loading() {
  return <p className="muted">Loading…</p>;
}

/** Failures are shown rather than swallowed: an empty page when a service is
 *  down reads as "there is nothing here", which is a different thing. */
export function Failed({ error, onRetry }: { error: string; onRetry?: () => void }) {
  return (
    <div className="failed" role="alert">
      <p>{error}</p>
      {onRetry ? (
        <button type="button" onClick={onRetry}>
          Try again
        </button>
      ) : null}
    </div>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="muted">{children}</p>;
}

export function ProductCard({ product }: { product: Product }) {
  return (
    <article className="card">
      <h3>
        <Link to={`/products/${product.id}`}>{product.name}</Link>
      </h3>
      <p className="muted">{product.category.join(", ")}</p>
      <p className="price">{price(product.price)}</p>
    </article>
  );
}
