import type { GlobalThemeOverrides } from 'naive-ui'

/**
 * NekoNest 可爱风主题覆盖
 * 主色：淡紫 + 浅薄荷
 * 背景：米白
 * 风格：大圆角、柔和阴影
 */
export const nekoThemeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: '#B8A9E8',
    primaryColorHover: '#A594DB',
    primaryColorPressed: '#9280CE',
    primaryColorSuppl: '#C9BBEF',
    
    successColor: '#7ED4A6',
    successColorHover: '#6BC495',
    successColorPressed: '#5AB484',
    
    warningColor: '#F4D4A0',
    warningColorHover: '#E8C890',
    warningColorPressed: '#DCBC80',
    
    errorColor: '#F4A0A0',
    errorColorHover: '#E89090',
    errorColorPressed: '#DC8080',
    
    infoColor: '#A0C8F4',
    infoColorHover: '#90B8E8',
    infoColorPressed: '#80A8DC',
    
    textColorBase: '#4A4A4A',
    textColor1: '#4A4A4A',
    textColor2: '#6A6A6A',
    textColor3: '#9E9E9E',
    
    borderColor: '#E8E4E0',
    borderRadius: '12px',
    borderRadiusSmall: '8px',
    
    boxShadow1: '0 2px 8px rgba(0, 0, 0, 0.06)',
    boxShadow2: '0 4px 16px rgba(0, 0, 0, 0.08)',
    
    fontFamily: '"Noto Sans SC", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
    
    bodyColor: '#FAF8F5',
    cardColor: '#FFFFFF',
    modalColor: '#FFFFFF',
    popoverColor: '#FFFFFF',
    tableColor: '#FFFFFF',
  },
  
  Button: {
    borderRadiusMedium: '20px',
    borderRadiusLarge: '24px',
    borderRadiusSmall: '16px',
    
    colorPrimary: '#B8A9E8',
    colorHoverPrimary: '#A594DB',
    colorPressedPrimary: '#9280CE',
    
    fontWeight: '500',
  },
  
  Card: {
    borderRadius: '16px',
    borderColor: '#E8E4E0',
    color: '#FFFFFF',
    paddingMedium: '16px',
  },
  
  Tag: {
    borderRadius: '12px',
  },
  
  Input: {
    borderRadius: '12px',
  },
}
