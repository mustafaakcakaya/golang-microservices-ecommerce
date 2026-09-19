import { NavLink, Route, Routes } from "react-router-dom";

import { Cart } from "./pages/Cart";
import { Checkout } from "./pages/Checkout";
import { Confirmation } from "./pages/Confirmation";
import { Home } from "./pages/Home";
import { Orders } from "./pages/Orders";
import { ProductDetail } from "./pages/ProductDetail";
import { Products } from "./pages/Products";

export function App() {
  return (
    <div className="page">
      <header>
        <NavLink to="/" className="brand">
          Storefront
        </NavLink>
        <nav>
          <NavLink to="/products">Products</NavLink>
          <NavLink to="/cart">Cart</NavLink>
          <NavLink to="/orders">Orders</NavLink>
        </nav>
      </header>

      <main>
        <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/products" element={<Products />} />
          <Route path="/products/:id" element={<ProductDetail />} />
          <Route path="/cart" element={<Cart />} />
          <Route path="/checkout" element={<Checkout />} />
          <Route path="/confirmation" element={<Confirmation />} />
          <Route path="/orders" element={<Orders />} />
          <Route path="*" element={<p>That page does not exist.</p>} />
        </Routes>
      </main>
    </div>
  );
}
