import React from 'react';

type TextAreaProps = {
  id?: string;
  name?: string;
  className?: string;
  placeholder?: string;
  rows?: number;
  content?: string;
};

const TextArea: React.FC<TextAreaProps> = ({
  id,
  name,
  className,
  placeholder,
  rows,
  content
}) => {
  return (
    <textarea
      id={id}
      name={name}
      className={className}
      placeholder={placeholder}
      rows={rows}
      defaultValue={content}
    />
  );
};

export default TextArea;
