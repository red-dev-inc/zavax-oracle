import React from "react";

interface TextLoadingIndicatorProps {
  loading: boolean;
}

const TextLoadingIndicator: React.FC<TextLoadingIndicatorProps> = ({
  loading,
}) => {
  if (!loading) return null;
  return (
    <div className="loading-animation">
      <span className="dot">.</span>
      <span className="dot">.</span>
      <span className="dot">.</span>
    </div>
  );
};

export default TextLoadingIndicator;
