import React, { ChangeEvent } from 'react';

type TextFieldProps = {
  id?: string;
  name?: string;
  className?: string;
  placeholder?: string;
  onChange: (event: ChangeEvent<HTMLInputElement>) => void;
};

const TextField: React.FC<TextFieldProps> = ({
  id,
  name,
  className,
  placeholder,
  onChange
}) => {
  const handleChange = (e: ChangeEvent<HTMLInputElement>) => {
    onChange(e);
  };

  return (
    <input
      id={id}
      name={name}
      className={className}
      placeholder={placeholder}
      onChange={handleChange}
    />
  );
};

export default TextField;
