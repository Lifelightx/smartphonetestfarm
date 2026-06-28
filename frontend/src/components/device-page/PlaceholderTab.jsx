import React from 'react';

function PlaceholderTab({ icon: Icon, title, description, colorClass }) {
  return (
    <div className="feature-placeholder">
      <div className={`feature-placeholder-icon ${colorClass || ''}`}>
        <Icon size={32} />
      </div>
      <h4>{title}</h4>
      <p>{description}</p>
    </div>
  );
}

export default PlaceholderTab;
