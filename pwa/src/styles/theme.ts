import type { GlobalThemeOverrides } from 'naive-ui'

/**
 * NekoNest Naive UI theme. Purple is interactive primary;
 * rose stays brand decoration.
 */
export const nekoThemeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: '#725B9D',
    primaryColorHover: '#7F66AB',
    primaryColorPressed: '#65488E',
    primaryColorSuppl: '#725B9D',

    successColor: '#70A98E',
    successColorHover: '#619B7F',
    successColorPressed: '#528B70',

    warningColor: '#BC8448',
    warningColorHover: '#AC743B',
    warningColorPressed: '#986431',

    errorColor: '#BF6874',
    errorColorHover: '#AF5966',
    errorColorPressed: '#994B58',

    infoColor: '#758EB4',
    infoColorHover: '#667FA5',
    infoColorPressed: '#596F91',

    textColorBase: '#463B48',
    textColor1: '#463B48',
    textColor2: '#5F5460',
    textColor3: '#6E6270',
    placeholderColor: '#6E6270',

    borderColor: 'rgba(110, 89, 119, 0.18)',
    borderRadius: '14px',
    borderRadiusSmall: '9px',

    boxShadow1: '0 8px 24px rgba(92, 67, 92, 0.09)',
    boxShadow2: '0 18px 46px rgba(92, 67, 92, 0.13)',

    fontFamily:
      '"Microsoft YaHei UI", "PingFang SC", "Hiragino Sans GB", "Noto Sans CJK SC", "Segoe UI Variable Text", system-ui, sans-serif',

    bodyColor: '#F8F1ED',
    cardColor: '#FFFAF8',
    modalColor: '#FFFAF8',
    popoverColor: '#FFFAF8',
    tableColor: '#FFFAF8'
  },

  Button: {
    borderRadiusMedium: '13px',
    borderRadiusLarge: '15px',
    borderRadiusSmall: '10px',

    colorPrimary: '#725B9D',
    colorHoverPrimary: '#7F66AB',
    colorPressedPrimary: '#65488E',

    fontWeight: '650'
  },

  Card: {
    borderRadius: '18px',
    borderColor: 'rgba(110, 89, 119, 0.15)',
    color: '#FFFAF8',
    paddingMedium: '16px'
  },

  Tag: {
    borderRadius: '9px'
  },

  Input: {
    borderRadius: '12px',
    placeholderColor: '#6E6270'
  }
}

export const nekoThemeOverridesDark: GlobalThemeOverrides = {
  common: {
    primaryColor: '#C4B0E8',
    primaryColorHover: '#D2C2F0',
    primaryColorPressed: '#B09AD8',
    primaryColorSuppl: '#C4B0E8',

    successColor: '#7EC9A4',
    warningColor: '#D8A86A',
    errorColor: '#E08B96',
    infoColor: '#9AB0D0',

    textColorBase: '#F0EBF5',
    textColor1: '#F0EBF5',
    textColor2: '#B9B1C6',
    textColor3: '#91899E',
    placeholderColor: '#91899E',

    borderColor: 'rgba(196, 176, 232, 0.16)',
    borderRadius: '14px',
    borderRadiusSmall: '9px',

    boxShadow1: '0 8px 24px rgba(0, 0, 0, 0.38)',
    boxShadow2: '0 18px 46px rgba(0, 0, 0, 0.5)',

    fontFamily:
      '"Microsoft YaHei UI", "PingFang SC", "Hiragino Sans GB", "Noto Sans CJK SC", "Segoe UI Variable Text", system-ui, sans-serif',

    bodyColor: '#1A1620',
    cardColor: '#221E2C',
    modalColor: '#221E2C',
    popoverColor: '#2B2636',
    tableColor: '#221E2C'
  },

  Button: {
    borderRadiusMedium: '13px',
    borderRadiusLarge: '15px',
    borderRadiusSmall: '10px',
    colorPrimary: '#C4B0E8',
    colorHoverPrimary: '#D2C2F0',
    colorPressedPrimary: '#B09AD8',
    textColorPrimary: '#1A1422',
    textColorHoverPrimary: '#1A1422',
    textColorPressedPrimary: '#1A1422',
    fontWeight: '650'
  },

  Card: {
    borderRadius: '18px',
    borderColor: 'rgba(196, 176, 232, 0.14)',
    color: '#221E2C',
    paddingMedium: '16px'
  },

  Tag: {
    borderRadius: '9px'
  },

  Input: {
    borderRadius: '12px',
    placeholderColor: '#91899E',
    color: '#2B2636',
    colorFocus: '#322C3E',
    textColor: '#F0EBF5',
    border: '1px solid rgba(196, 176, 232, 0.16)',
    borderHover: '1px solid rgba(196, 176, 232, 0.28)',
    borderFocus: '1px solid rgba(196, 176, 232, 0.45)'
  }
}
