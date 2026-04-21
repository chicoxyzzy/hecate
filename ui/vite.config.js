import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
export default defineConfig({
    plugins: [react(), tailwindcss()],
    server: {
        port: 5173,
        proxy: {
            "/healthz": "http://127.0.0.1:8080",
            "/v1": "http://127.0.0.1:8080",
            "/admin": "http://127.0.0.1:8080",
        },
    },
});
