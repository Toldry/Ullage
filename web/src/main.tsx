import { render } from 'preact';
import './styles/fonts.css';
import './styles/theme.css';
import './styles/app.css';
import { App } from './App';

render(<App />, document.getElementById('app')!);
