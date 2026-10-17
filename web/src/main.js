import { mount } from 'svelte';
import App from './App.svelte';
import './app.css';
import '../artifact-upload.js';

mount(App, { target: document.getElementById('app') });
