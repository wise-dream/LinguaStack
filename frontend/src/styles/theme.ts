// Темы Naive UI из токенов LinguaStack. Naive UI считает оттенки наведения
// и нажатия из настоящих цветов, поэтому палитры повторяют tokens.css —
// расхождение ловит theme.test.ts.
import type { GlobalThemeOverrides } from 'naive-ui'

interface Palette {
  paper: string
  paperRaised: string
  surface: string
  inkOriginal: string
  inkTranslation: string
  proof: string
  highlight: string
  rule: string
  ruleSoft: string
  muted: string
  warn: string
}

// Ночная палитра — то же издание при лампе: бумага сине-грифельная, а не
// чёрная; краски оригинала и перевода светлеют, но остаются различимы.
// Текст чуть мягче белого — для долгого чтения.
export const palettes = {
  light: {
    paper: '#f4f6f3',
    paperRaised: '#fbfcfa',
    surface: '#ffffff',
    inkOriginal: '#1b2440',
    inkTranslation: '#14463f',
    proof: '#b3261e',
    highlight: '#f3d35c',
    rule: '#c8cec6',
    ruleSoft: '#e3e7e1',
    muted: '#5e6a66',
    warn: '#6b5600',
  },
  dark: {
    paper: '#12161c',
    paperRaised: '#171c23',
    surface: '#1c2229',
    inkOriginal: '#d5dce8',
    inkTranslation: '#8fd0c0',
    proof: '#f28b82',
    highlight: '#d9b44a',
    rule: '#4a5461',
    ruleSoft: '#29303a',
    muted: '#9aa4a0',
    warn: '#e0c25c',
  },
} as const satisfies Record<'light' | 'dark', Palette>

/** Оттенки наведения и нажатия, которых нет среди токенов. */
interface Shades {
  primaryHover: string
  primaryPressed: string
  successHover: string
  successPressed: string
  errorHover: string
  errorPressed: string
  warning: string
  warningHover: string
  warningPressed: string
  placeholder: string
  popover: string
  /** Заливка полей ввода. */
  input: string
}

const shades: Record<'light' | 'dark', Shades> = {
  light: {
    primaryHover: '#2c3a63',
    primaryPressed: '#111830',
    successHover: '#1f6156',
    successPressed: '#0c2f2a',
    errorHover: '#cc3a31',
    errorPressed: '#8d1c16',
    warning: '#9a6400',
    warningHover: '#b47700',
    warningPressed: '#7a4f00',
    placeholder: '#8b9590',
    popover: '#ffffff',
    input: '#ffffff',
  },
  dark: {
    primaryHover: '#eef1f7',
    primaryPressed: '#b6c0d2',
    successHover: '#abdfd2',
    successPressed: '#6fb4a4',
    errorHover: '#f6a7a0',
    errorPressed: '#d9706a',
    warning: '#e2b04a',
    warningHover: '#ebc26e',
    warningPressed: '#c8952f',
    placeholder: '#7a8581',
    popover: '#232a33',
    // Чуть темнее карточки: поле читается углублением.
    input: '#161b21',
  },
}

const fontUi = "'Golos Text Variable', 'Golos Text', system-ui, -apple-system, 'Segoe UI', sans-serif"

function overrides(p: Palette, s: Shades): GlobalThemeOverrides {
  return {
    common: {
      fontFamily: fontUi,
      fontSize: '15px',
      fontSizeMedium: '15px',
      fontSizeSmall: '13px',
      lineHeight: '1.5',
      borderRadius: '6px',
      borderRadiusSmall: '4px',
      heightMedium: '38px',
      heightSmall: '30px',
      // Текст на основных кнопках: в тёмной теме кнопка светлая.
      baseColor: p.surface,
      primaryColor: p.inkOriginal,
      primaryColorHover: s.primaryHover,
      primaryColorPressed: s.primaryPressed,
      primaryColorSuppl: s.primaryHover,
      infoColor: p.inkOriginal,
      infoColorHover: s.primaryHover,
      infoColorPressed: s.primaryPressed,
      infoColorSuppl: s.primaryHover,
      successColor: p.inkTranslation,
      successColorHover: s.successHover,
      successColorPressed: s.successPressed,
      successColorSuppl: s.successHover,
      errorColor: p.proof,
      errorColorHover: s.errorHover,
      errorColorPressed: s.errorPressed,
      errorColorSuppl: s.errorHover,
      warningColor: s.warning,
      warningColorHover: s.warningHover,
      warningColorPressed: s.warningPressed,
      warningColorSuppl: s.warningHover,
      textColorBase: p.inkOriginal,
      textColor1: p.inkOriginal,
      textColor2: p.inkOriginal,
      textColor3: p.muted,
      placeholderColor: s.placeholder,
      bodyColor: p.paper,
      cardColor: p.surface,
      modalColor: p.surface,
      popoverColor: s.popover,
      inputColor: s.input,
      tableColor: p.surface,
      borderColor: p.rule,
      dividerColor: p.ruleSoft,
    },
    // В тёмной теме Naive поля без рамки, одной заливкой; здесь рамка в
    // обеих темах — как у карточек и таблиц.
    Input: {
      border: `1px solid ${p.rule}`,
    },
    InternalSelection: {
      border: `1px solid ${p.rule}`,
    },
    Button: {
      fontWeight: '500',
      paddingMedium: '0 16px',
    },
    Card: {
      borderRadius: '10px',
      borderColor: p.ruleSoft,
    },
    Table: {
      borderColor: p.ruleSoft,
      thColor: p.surface,
      thTextColor: p.muted,
      thFontWeight: '500',
    },
    Tag: {
      borderRadius: '999px',
    },
    Alert: {
      borderRadius: '8px',
    },
  }
}

export const themeOverrides = {
  light: overrides(palettes.light, shades.light),
  dark: overrides(palettes.dark, shades.dark),
}
