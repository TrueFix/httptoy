import { renderToString } from 'react-dom/server';
import App, { questions } from './App';

export function render() {
  return { html: renderToString(<App />), faq: { '@context': 'https://schema.org', '@type': 'FAQPage', mainEntity: questions.map(([question, answer]) => ({ '@type': 'Question', name: question, acceptedAnswer: { '@type': 'Answer', text: answer } })) } };
}
