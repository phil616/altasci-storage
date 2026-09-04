import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
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
