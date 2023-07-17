import React from 'react';

type TextAreaProps = {
  id?: string;
  name?: string;
  className?: string;
  placeholder?: string;
  rows?: number;
  readonly?:boolean;
  content?: string;
};

const TextArea: React.FC<TextAreaProps> = ({
  id,
  name,
  className,
  placeholder,
  rows,
  readonly,
  content
}) => {
  return (
    <textarea
      id={id}
      name={name}
      className={className}
      placeholder={placeholder}
      rows={rows}
      readOnly={readonly}
      defaultValue={content}
    />
  );
};

export default TextArea;
