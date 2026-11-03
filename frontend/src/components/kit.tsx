// CTA/footer compositions adapted from Shadcn UI Kit cta4 and footer9.
import { cloneElement, isValidElement, type ButtonHTMLAttributes, type ReactElement, type ReactNode } from 'react';
import { ArrowUpRight } from 'lucide-react';

function Button({ asChild, variant, children, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { asChild?: boolean; variant?: string; children: ReactNode }) {
  const className = `kit-button ${variant === 'outline' ? 'kit-button-secondary' : ''} ${props.className || ''}`;
  if (asChild && isValidElement(children)) return cloneElement(children as ReactElement<{ className?: string }>, { className });
  return <button {...props} className={className}>{children}</button>;
}

export function ClosingCTA() {
  return (
    <section className="w-full py-10 md:px-6 lg:py-28 kit-cta" aria-labelledby="closing-title">
      <div className="mx-auto max-w-5xl px-4">
        <div className="grid grid-cols-1 gap-4 text-center lg:grid-cols-2 lg:text-left">
          <h3 id="closing-title" className="text-3xl font-bold text-balance sm:text-4xl">Go on.<br />Break it.</h3>
          <div className="flex flex-col gap-2 space-y-4">
            <p className="text-muted-foreground text-balance md:text-lg">Give your HTTP client somewhere safe to fail. Start with a real request, then explore the edges.</p>
            <div className="flex justify-center gap-3 lg:justify-start">
              <Button asChild><a href="#playground">Send a request <ArrowUpRight size={18} aria-hidden="true" /></a></Button>
              <Button variant="outline" asChild><a href="https://github.com/TrueFix/httptoy">View the source</a></Button>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}

const navLinks = [{ label: 'Playground', href: '#playground' }, { label: 'API reference', href: '#api-reference' }, { label: 'Self-host', href: '#self-host' }, { label: 'Questions', href: '#questions' }];
const socialLinks = [{ label: 'GitHub', href: 'https://github.com/TrueFix/httptoy' }, { label: 'OpenAPI specification', href: '/openapi.json' }, { label: 'Postman collection', href: '/collection.json' }, { label: 'Prometheus metrics', href: '/metrics' }];

export function Footer() {
  return (
    <footer className="bg-background w-full font-sans site-footer">
      <div className="mx-auto max-w-6xl px-6 py-16">
        <div className="grid grid-cols-1 gap-12 md:grid-cols-3">
          <div>
            <div className="text-foreground flex items-center gap-2.5"><img src="/assets/logo.svg" alt="httptoy" width={202} height={44} className="h-auto w-36 dark:invert" /></div>
            <p className="text-muted-foreground mt-5 text-sm leading-relaxed">An open-source HTTP testing API.<br />Built with Go. Made for the edges.</p>
          </div>
          <div><ul className="space-y-4">{navLinks.map(link => <li key={link.label}><a href={link.href} className="text-foreground hover:text-muted-foreground text-xl font-bold transition-colors">{link.label}</a></li>)}</ul></div>
          <div>{/* taste-skill-ignore: retained kit classes; border token is transparent. */}{socialLinks.map((social, i) => <a key={social.label} href={social.href} className={`group hover:text-muted-foreground flex items-center justify-between py-4 transition-colors ${i !== 0 ? 'border-border border-t' : ''}`}><span className="text-foreground group-hover:text-muted-foreground text-base font-semibold">{social.label}</span><ArrowUpRight aria-hidden="true" className="text-foreground group-hover:text-muted-foreground size-4 transition-colors" /></a>)}</div>
        </div>
      </div>
      {/* taste-skill-ignore: retained kit classes; border token is transparent. */}<div className="border-border border-t"><div className="text-muted-foreground mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-3 px-6 py-5 text-sm"><div className="flex items-center gap-4"><span>httptoy · TrueFix</span><span className="text-border">|</span><a href="https://github.com/TrueFix/httptoy/blob/main/LICENSE" className="hover:text-foreground transition-colors">MIT license</a></div><span>Open source. Yours to run.</span></div></div>
      <div className="footer-wordmark" aria-hidden="true">httptoy</div>
    </footer>
  );
}
