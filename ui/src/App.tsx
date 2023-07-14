import './App.css';
import Footer from './components/organisms/Footer';
import Header from './components/organisms/Header';
import Home from './components/pages/Home';

function App() {
  return (
    <div className="container">
      <div className="row p-4">
        <Header />
        <Home />
        <Footer />
      </div>
    </div>
  );
}

export default App;
