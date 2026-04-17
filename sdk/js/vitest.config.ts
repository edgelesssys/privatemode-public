import { defineConfig } from 'vitest/config';

export default defineConfig({
  define: {
    'import.meta.env.PRIVATEMODE_API_KEY': JSON.stringify(
      process.env.PRIVATEMODE_API_KEY ?? '',
    ),
    'import.meta.env.PRIVATEMODE_RUN_INTEGRATION_TESTS': JSON.stringify(
      process.env.PRIVATEMODE_RUN_INTEGRATION_TESTS ?? '',
    ),
  },
  test: {
    include: ['src/**/*.test.ts'],
  },
});
