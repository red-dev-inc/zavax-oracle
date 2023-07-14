import React, { FC } from 'react';

type ButtonProps = {
  id: string;
  text: string;
  className: string;
  onClick: () => void; // Add this line
  disabled?: boolean
}


const Button: FC<ButtonProps> = ({ id, text, className, onClick, ...rest }) => {
  return (
    <button id={id} className={className} onClick={onClick} {...rest}>
      {text}
    </button>
  );
};

export default Button;
