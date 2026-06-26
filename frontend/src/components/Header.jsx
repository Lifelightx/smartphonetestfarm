import React from 'react';

function Header({ theme, toggleTheme }) {
  return (
    <header className="header">
      <div className="header-logo">🤖</div>
      <h1 className="header-title">Protean STF Portal</h1>
      <div className="header-subtitle">Live Device Farm</div>
      <button className="theme-toggle-btn" onClick={toggleTheme}>
        {theme === 'dark' ? '☀️ Light' : '🌙 Dark'}
      </button>
    </header>
  );
}

export default Header;
