import type { ThemeConfig } from "antd";

export const enterpriseTheme: ThemeConfig = {
  token: {
    colorPrimary: "#1677ff",
    colorInfo: "#1677ff",
    colorSuccess: "#52c41a",
    colorWarning: "#faad14",
    colorError: "#ff4d4f",
    colorBgLayout: "#f5f5f5",
    colorText: "#1f1f1f",
    colorTextSecondary: "#8c8c8c",
    borderRadius: 6,
    borderRadiusLG: 8,
    controlHeight: 36,
    fontFamily: 'Inter, "Noto Sans SC", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  },
  components: {
    Layout: {
      headerBg: "#ffffff",
      siderBg: "#ffffff",
      bodyBg: "#f5f5f5",
    },
    Menu: {
      itemBorderRadius: 6,
    },
    Card: {
      headerBg: "transparent",
    },
    Table: {
      headerBg: "#fafafa",
    },
  },
};
