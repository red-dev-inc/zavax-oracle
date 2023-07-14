import React from 'react';

type LabelProps = {
  text: string;
  className: string;
}

const Label: React.FC<LabelProps> = ({ text, className }) => {
  return <div className={className}>{text}</div>;
};

export default Label;
