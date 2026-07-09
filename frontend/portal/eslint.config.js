import js from '@eslint/js'
import prettier from 'eslint-config-prettier'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import globals from 'globals'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['dist', 'node_modules'] },
  {
    files: ['**/*.{ts,tsx}'],
    extends: [js.configs.recommended, ...tseslint.configs.recommended, prettier],
    languageOptions: {
      ecmaVersion: 2022,
      globals: globals.browser,
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
      // Contract C8 / NFR-6.2: logical Tailwind utilities only (see
      // @hikrad/shared eslint.config.js — same rule, same reason).
      'no-restricted-syntax': [
        'error',
        {
          selector:
            'JSXAttribute[name.name="className"] Literal[value=/\\b(?:m|p)[lr]-|\\b(?:left|right)-\\d|\\btext-(?:left|right)\\b|\\bfloat-(?:left|right)\\b|\\b(?:border|rounded)-[lr](?:-|\\b)/]',
          message:
            'Physical left/right Tailwind utility — use logical variants (ms-/me-/ps-/pe-/start-/end-/text-start/text-end/border-s/border-e/rounded-s/rounded-e) so RTL mirrors (contract C8).',
        },
      ],
    },
  },
)
