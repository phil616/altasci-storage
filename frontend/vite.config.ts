import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";

// Share URLs use the Web origin, which may differ from the API origin.
function cliDiscovery(): Plugin {
  let source = "";
  return {
    name: "altasci-cli-discovery",
    configResolved(config) {
      source = JSON.stringify({ api_url: config.env.VITE_API_BASE_URL || "https://loopback-api.altasci.com" }) + "\n";
    },
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        if (req.url?.split("?")[0] !== "/.well-known/altasci.json") return next();
        res.setHeader("Content-Type", "application/json");
        res.setHeader("Cache-Control", "no-store");
        res.end(source);
      });
    },
    generateBundle() {
      this.emitFile({ type: "asset", fileName: ".well-known/altasci.json", source });
    },
  };
}

export default defineConfig({
  plugins: [react(), cliDiscovery()],
  build: {
    target: "es2022",
    sourcemap: true,
    rolldownOptions: {
      output: {
        // Keep route-level dynamic imports, but let Rolldown preserve the
        // initialization order of Ant Design and @rc-component internals.
        codeSplitting: true,
      },
    },
  },
});
