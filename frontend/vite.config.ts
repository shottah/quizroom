import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// In development, `npm run dev` proxies the API to the running stack.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8790",
      "/openapi.json": "http://127.0.0.1:8790",
    },
  },
});
