import { Link } from "react-router-dom";

import { catalogApi } from "../api/services";
import { Failed, Loading, ProductCard } from "../components";
import { useAsync } from "../useAsync";

export function Home() {
  const { data: products, error, loading, reload } = useAsync(() => catalogApi.products(3));

  return (
    <>
      <section className="banner">
        <h1>Everything you need, in one place</h1>
        <p>A small catalog, a basket that remembers, and orders that survive a restart.</p>
        <Link to="/products" className="button">
          Browse the catalog
        </Link>
      </section>

      <h2>Featured</h2>
      {loading ? <Loading /> : null}
      {error ? <Failed error={error} onRetry={reload} /> : null}
      <div className="grid">
        {products?.map((product) => <ProductCard key={product.id} product={product} />)}
      </div>
    </>
  );
}
