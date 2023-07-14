import React from 'react';
import logo from '../../assets/img/RedBridge-ZCash+Avax.png';

const Header: React.FC = () => {
  return (
    <header className="mb-5">
      <div className="row align-items-center">
        <div className="col">
          <h1 className='mainTitle'>ZavaX Subnet Oracle</h1>
        </div>
        <div className="col-auto">
          <img src={logo} alt="Logo" className="logo" />
        </div>
      </div>
    </header>
  );
};

export default Header;
