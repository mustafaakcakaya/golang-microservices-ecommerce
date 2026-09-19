import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import { basketApi, catalogApi } from "../api/services";
import { ApiError } from "../api/client";
import type { CartItem } from "../api/types";
import { Failed, Loading, price } from "../components";
import { shopper } from "../config";
import { useAsync } from "../useAsync";

export function ProductDetail() {
  const { id = "" } = useParams();
  const navigate = useNavigate();

  const { data: product, error, loading, reload } = useAsync(() => catalogApi.product(id), [id]);
  const [quantity, setQuantity] = useState(1);
  const [adding, setAdding] = useState(false);
  const [failure, setFailure] = useState<string>();

  async function addToCart() {
    if (!product) {
      return;
    }

    setAdding(true);
    setFailure(undefined);

    try {
      // The basket is stored whole, so the current lines are read first and
      // the new one merged into them.
      const existing = await currentItems();
      const line: CartItem = {
        quantity,
        color: "",
        price: product.price,
        productId: product.id,
        productName: product.name,
      };

      const merged = existing.filter((item) => item.productId !== product.id);
      const already = existing.find((item) => item.productId === product.id);
      if (already) {
        line.quantity += already.quantity;
      }

      await basketApi.store({ userName: shopper.userName, items: [...merged, line] });
      navigate("/cart");
    } catch (addError: unknown) {
      setFailure(addError instanceof Error ? addError.message : String(addError));
    } finally {
      setAdding(false);
    }
  }

  if (loading) {
    return <Loading />;
  }
  if (error || !product) {
    return <Failed error={error ?? "that product does not exist"} onRetry={reload} />;
  }

  return (
    <article className="detail">
      <h2>{product.name}</h2>
      <p className="muted">{product.category.join(", ")}</p>
      <p>{product.description}</p>
      <p className="price">{price(product.price)}</p>

      <div className="row">
        <label htmlFor="quantity">Quantity</label>
        <input
          id="quantity"
          type="number"
          min={1}
          value={quantity}
          onChange={(event) => setQuantity(Math.max(1, Number(event.target.value)))}
        />
        <button type="button" onClick={addToCart} disabled={adding}>
          {adding ? "Adding…" : "Add to cart"}
        </button>
      </div>

      {failure ? <Failed error={failure} /> : null}
    </article>
  );
}

/** currentItems reads the basket, treating "no basket yet" as an empty one. */
async function currentItems(): Promise<CartItem[]> {
  try {
    return (await basketApi.get(shopper.userName)).items ?? [];
  } catch (error: unknown) {
    if (error instanceof ApiError && error.status === 404) {
      return [];
    }
    throw error;
  }
}
