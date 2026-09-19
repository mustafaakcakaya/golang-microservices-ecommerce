import { catalogApi } from "../api/services";
import { Empty, Failed, Loading, ProductCard } from "../components";
import { useAsync } from "../useAsync";

export function Products() {
  const { data: products, error, loading, reload } = useAsync(() => catalogApi.products());

  if (loading) {
    return <Loading />;
  }
  if (error) {
    return <Failed error={error} onRetry={reload} />;
  }
  if (!products?.length) {
    return <Empty>The catalog is empty.</Empty>;
  }

  return (
    <>
      <h2>Products</h2>
      <div className="grid">
        {products.map((product) => <ProductCard key={product.id} product={product} />)}
      </div>
    </>
  );
}
