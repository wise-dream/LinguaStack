import pluginVue from 'eslint-plugin-vue'
import tseslint from 'typescript-eslint'
import vueParser from 'vue-eslint-parser'

export default [
  { ignores: ['dist/**', 'node_modules/**', 'playwright-report/**', 'test-results/**'] },
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/recommended'],
  {
    files: ['**/*.vue'],
    languageOptions: {
      parser: vueParser,
      parserOptions: { parser: tseslint.parser, sourceType: 'module', extraFileExtensions: ['.vue'] },
    },
  },
  {
    rules: {
      // Страницы называются одним словом по смыслу: App, Login.
      'vue/multi-word-component-names': 'off',
      // v-html — только через renderInline (экранирует сам).
      'vue/no-v-html': 'error',
    },
  },
]
