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
    primaryColor: '#B5A0D8',
    primaryColorHover: '#C4B2E2',
    primaryColorPressed: '#9A84C4',
    primaryColorSuppl: '#B5A0D8',

    successColor: '#7FBE9E',
    warningColor: '#D0A06A',
    errorColor: '#D48892',
    infoColor: '#8FA6C8',

    textColorBase: '#F2EBEF',
    textColor1: '#F2EBEF',
    textColor2: '#C9BDC6',
    textColor3: '#AFA3AC',
    placeholderColor: '#AFA3AC',

    borderColor: 'rgba(210, 190, 210, 0.18)',
    borderRadius: '14px',
    borderRadiusSmall: '9px',

    boxShadow1: '0 8px 24px rgba(0, 0, 0, 0.35)',
    boxShadow2: '0 18px 46px rgba(0, 0, 0, 0.45)',

    fontFamily:
      '"Microsoft YaHei UI", "PingFang SC", "Hiragino Sans GB", "Noto Sans CJK SC", "Segoe UI Variable Text", system-ui, sans-serif',

    bodyColor: '#1C171C',
    cardColor: '#261F26',
    modalColor: '#261F26',
    popoverColor: '#2C242C',
    tableColor: '#261F26'
  },

  Button: {
    borderRadiusMedium: '13px',
    borderRadiusLarge: '15px',
    borderRadiusSmall: '10px',
    colorPrimary: '#B5A0D8',
    colorHoverPrimary: '#C4B2E2',
    colorPressedPrimary: '#9A84C4',
    fontWeight: '650'
  },

  Card: {
    borderRadius: '18px',
    borderColor: 'rgba(210, 190, 210, 0.16)',
    color: '#261F26',
    paddingMedium: '16px'
  },

  Tag: {
    borderRadius: '9px'
  },

  Input: {
    borderRadius: '12px',
    placeholderColor: '#AFA3AC',
    color: '#2C242C',
    colorFocus: '#322A32'
  }
}
