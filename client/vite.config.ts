import { defineConfig } from "vite";
import solid from "vite-plugin-solid";

// In dev, proxy the backend API paths to the Go server so the browser talks to it
// same-origin (the backend has no CORS). Override the target with BACKEND_URL.
// Set VITE_DS_HTTP_URL="" so the API clients use relative same-origin paths.
const backend = process.env.BACKEND_URL ?? "http://localhost:18080";
const apiPaths = ["/auth", "/workspaces", "/conversations", "/devices", "/keypackages", "/kt"];

export default defineConfig({
  plugins: [solid()],
  worker: { format: "es" },
  server: {
    proxy: {
      ...Object.fromEntries(apiPaths.map((p) => [p, { target: backend, changeOrigin: true }])),
      "/ws": { target: backend, ws: true, changeOrigin: true },
    },
  },
});
