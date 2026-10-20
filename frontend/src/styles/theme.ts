// Тема Naive UI из токенов LinguaStack. Naive UI считает оттенки наведения
// и нажатия из настоящих цветов, поэтому палитра повторяет tokens.css —
// расхождение ловит theme.test.ts.
import type { GlobalThemeOverrides } from 'naive-ui'

export const palette = {
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
} as const

const fontUi = "'Golos Text Variable', 'Golos Text', system-ui, -apple-system, 'Segoe UI', sans-serif"

export const themeOverrides: GlobalThemeOverrides = {
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
    primaryColor: palette.inkOriginal,
    primaryColorHover: '#2c3a63',
    primaryColorPressed: '#111830',
    primaryColorSuppl: '#2c3a63',
    infoColor: palette.inkOriginal,
    infoColorHover: '#2c3a63',
    infoColorPressed: '#111830',
    infoColorSuppl: '#2c3a63',
    successColor: palette.inkTranslation,
    successColorHover: '#1f6156',
    successColorPressed: '#0c2f2a',
    successColorSuppl: '#1f6156',
    errorColor: palette.proof,
    errorColorHover: '#cc3a31',
    errorColorPressed: '#8d1c16',
    errorColorSuppl: '#cc3a31',
    warningColor: '#9a6400',
    warningColorHover: '#b47700',
    warningColorPressed: '#7a4f00',
    warningColorSuppl: '#b47700',
    textColorBase: palette.inkOriginal,
    textColor1: palette.inkOriginal,
    textColor2: palette.inkOriginal,
    textColor3: palette.muted,
    placeholderColor: '#8b9590',
    bodyColor: palette.paper,
    cardColor: palette.surface,
    inputColor: palette.surface,
    tableColor: palette.surface,
    borderColor: palette.rule,
    dividerColor: palette.ruleSoft,
  },
  Button: {
    fontWeight: '500',
    paddingMedium: '0 16px',
  },
  Card: {
    borderRadius: '10px',
    borderColor: palette.ruleSoft,
  },
  Table: {
    borderColor: palette.ruleSoft,
    thColor: palette.surface,
    thTextColor: palette.muted,
    thFontWeight: '500',
  },
  Tag: {
    borderRadius: '999px',
  },
  Alert: {
    borderRadius: '8px',
  },
}
