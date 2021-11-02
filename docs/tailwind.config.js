module.exports = {
  purge: [
    './src/**/*.vue',
    './src/index.html',
  ],
  darkMode: false, // or 'media' or 'class'
  theme: {
    extend: {
      colors: {
        primary: '#5783AB'
      },
      minWidth: {
        '96': '24rem',
      }
    },
  },
  variants: {
    extend: {
      display: ['group-hover'],
    }
  },
  plugins: [
    function ({ addComponents }) {
      addComponents({
        '.container': {
          marginLeft: 'auto',
          marginRight: 'auto',
          width: '100%',
          maxWidth: '100%',
          paddingLeft: '1rem',
          paddingRight: '1rem',
          '@screen md': {
            paddingLeft: '2.5rem',
            paddingRight: '2.5rem',
          },
          '@screen 2xl': {
            maxWidth: '2480px',
            paddingLeft: '1.5rem',
            paddingRight: '1.5rem',
          },
        }
      })
    }
  ],
}
