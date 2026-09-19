import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The storefront never talks to a service directly: every call goes to /api,
// which nginx forwards to the gateway in a deployed stack. `vite dev` forwards
// it too, so working locally and running in the stack behave the same.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/api": {
        target: process.env.VITE_GATEWAY_URL ?? "http://localhost:6105",
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, ""),
      },
    },
  },
});
