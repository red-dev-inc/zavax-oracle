import React from 'react';

type LinkProps = {
  text: string;
  className: string;
  href: string;
  isBlank?: '_blank' | '_self' | '_parent' | '_top';
}

const Link: React.FC<LinkProps> = ({
  text,
  className,
  href,
  isBlank = '_blank'
}) => {
  return (
    <a target={isBlank} className={className} href={href} rel="noreferrer">
      {text}
    </a>
  );
};

export default Link;
