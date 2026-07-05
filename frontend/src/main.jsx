import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App.jsx'
import './index.css'

// Patch global window.fetch to inject JWT authorization header and handle token expiration/invalidation
const originalFetch = window.fetch;
window.fetch = async function (url, options = {}) {
  const token = localStorage.getItem('token');
  if (token && url.toString().includes('/api/v1/')) {
    options.headers = {
      ...options.headers,
      'Authorization': `Bearer ${token}`
    };
  }
  
  const response = await originalFetch(url, options);
  
  // Clear token and notify app if the session is unauthorized/expired (excluding the login request itself)
  if (response.status === 401 && !url.toString().includes('/api/v1/auth/login')) {
    localStorage.removeItem('token');
    window.dispatchEvent(new Event('auth-unauthorized'));
  }
  
  return response;
};

ReactDOM.createRoot(document.getElementById('root')).render(
  <App />,
)

