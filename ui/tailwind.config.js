import daisyui from 'daisyui'

export default {
  content: [
    "./index.html",
    "./src/**/*.{vue,js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {},
  },
  plugins: [daisyui],
  daisyui: {
    themes: [
      {
        "business-light": {
          "color-scheme": "light",
          "primary": "#1C4E80",
          "secondary": "#7C909A",
          "accent": "#EA6947",
          "neutral": "#e5e7eb",
          "base-100": "#ffffff",
          "base-200": "#f9fafb",
          "base-300": "#f3f4f6",
          "base-content": "#1f2937",
          "info": "#0091D5",
          "success": "#6BB187",
          "warning": "#DBAE59",
          "error": "#AC3E31",
          "--rounded-box": "0.25rem",
          "--rounded-btn": ".125rem",
          "--rounded-badge": ".125rem"
        }
      },
      "business"
    ],
  }
}
