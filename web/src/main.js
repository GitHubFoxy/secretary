import { mount } from 'svelte';
import App from './App.svelte';
import './app.css';

if (location.search === '?update') history.replaceState(null, '', '/');
mount(App, { target: document.getElementById('app') });

if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js');
