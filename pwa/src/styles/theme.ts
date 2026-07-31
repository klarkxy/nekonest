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
    primaryColor: '#CBB8EF',
    primaryColorHover: '#D9CAF5',
    primaryColorPressed: '#B8A3E0',
    primaryColorSuppl: '#CBB8EF',

    successColor: '#86D0AC',
    warningColor: '#E0B176',
    errorColor: '#E898A2',
    infoColor: '#A0B4D4',

    textColorBase: '#F4F0F8',
    textColor1: '#F4F0F8',
    textColor2: '#C7BFD4',
    textColor3: '#9B93AB',
    placeholderColor: '#9B93AB',

    borderColor: 'rgba(203, 184, 239, 0.2)',
    borderRadius: '14px',
    borderRadiusSmall: '9px',

    boxShadow1: '0 8px 24px rgba(0, 0, 0, 0.42)',
    boxShadow2: '0 18px 46px rgba(0, 0, 0, 0.55)',

    fontFamily:
      '"Microsoft YaHei UI", "PingFang SC", "Hiragino Sans GB", "Noto Sans CJK SC", "Segoe UI Variable Text", system-ui, sans-serif',

    bodyColor: '#16131C',
    cardColor: '#262131',
    modalColor: '#262131',
    popoverColor: '#312B3D',
    tableColor: '#262131'
  },

  Button: {
    borderRadiusMedium: '13px',
    borderRadiusLarge: '15px',
    borderRadiusSmall: '10px',
    colorPrimary: '#CBB8EF',
    colorHoverPrimary: '#D9CAF5',
    colorPressedPrimary: '#B8A3E0',
    textColorPrimary: '#1A1422',
    textColorHoverPrimary: '#1A1422',
    textColorPressedPrimary: '#1A1422',
    fontWeight: '650'
  },

  Card: {
    borderRadius: '18px',
    borderColor: 'rgba(203, 184, 239, 0.18)',
    color: '#262131',
    paddingMedium: '16px'
  },

  Tag: {
    borderRadius: '9px'
  },

  Input: {
    borderRadius: '12px',
    placeholderColor: '#9B93AB',
    color: '#312B3D',
    colorFocus: '#3A3348',
    textColor: '#F4F0F8',
    border: '1px solid rgba(203, 184, 239, 0.2)',
    borderHover: '1px solid rgba(203, 184, 239, 0.32)',
    borderFocus: '1px solid rgba(203, 184, 239, 0.48)'
  }
}
