import React from 'react';
import Link from '../atoms/Link';

const Footer: React.FC = () => {
  return (
    <footer className="mb-4">
      <div className="row">
        <div className="col">

          <Link
            text={`ZavaX Oracle on GitHub`}
            className={`footer-link`}
            href={`https://github.com/red-dev-inc/zavax-oracle`}
            isBlank="_blank"
          />
        </div>
      </div>
    </footer>
  );
};

export default Footer;
