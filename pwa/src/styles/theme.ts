import type { GlobalThemeOverrides } from 'naive-ui'

/**
 * NekoNest 的 Naive UI 主题。全局只用灰紫作为交互主色，
 * 玫瑰色留给品牌装饰，避免功能状态与装饰色混在一起。
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
    textColor2: '#746975',
    textColor3: '#766A75',

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
    borderRadius: '12px'
  }
}
