import { ArrowRight, ArrowUpRight, Check, FileText, Laptop, Mail, PenLine, Send, ShieldCheck } from 'lucide-react'

import { GoogleSignIn } from '@/components/google-sign-in'
import { ThemeControl } from '@/components/theme-control'
import { repositoryURL, setupGuideURL } from '@/lib/project'
import type { Session } from '@/types/api'

export function LandingPage({ session }: { session: Session }) {
  const signupAvailable = session.signup_available === true
  const destination = session.authenticated ? '#drafts' : '#self-host'
  const action = session.authenticated ? 'Open your workspace' : 'Run Sefir yourself'
  const linkStyle = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-lg bg-primary px-5 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2'

  return (
    <div className="min-h-screen">
      <header id="home" className="mx-auto flex max-w-6xl items-center justify-between gap-4 px-6 py-5 sm:py-7">
        <a href="#home" aria-label="Sefir home" className="flex items-center gap-2.5 text-xl font-semibold tracking-tight">
          <span className="flex size-9 items-center justify-center rounded-xl bg-primary text-primary-foreground"><Mail aria-hidden="true" className="size-5" /></span>Sefir
        </a>
        <nav aria-label="Welcome navigation" className="flex items-center gap-2 sm:gap-5">
          <a href="#how-it-works" className="hidden text-sm text-muted-foreground hover:text-foreground sm:block">How it works</a>
          <a href={repositoryURL} className="hidden text-sm text-muted-foreground hover:text-foreground sm:block" target="_blank" rel="noreferrer">Source code</a>
          <ThemeControl />
          <a href={session.authenticated ? '#drafts' : '#signin'} className="rounded-lg px-2 py-2 text-sm font-medium hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">{session.authenticated ? 'Workspace' : 'Sign in'}</a>
        </nav>
      </header>
      <main>
        <section className="mx-auto grid max-w-6xl items-center gap-12 px-6 pb-20 pt-12 sm:pt-20 lg:grid-cols-[1fr_1fr] lg:gap-16 lg:pb-28">
          <div>
            <p className="mb-5 flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.18em] text-primary"><span className="size-1.5 rounded-full bg-primary" />Open-source outreach, on your terms</p>
            <h1 className="max-w-xl text-5xl leading-[1.06] font-semibold tracking-[-0.045em] sm:text-6xl lg:text-7xl">A better<br />first <span className="text-primary">hello.</span></h1>
            <p className="mt-6 max-w-md text-lg leading-relaxed text-muted-foreground">Sefir helps you turn your experience into a personal introduction. Shape your message and approve every email before it leaves through Gmail, in a workspace built to run on your own machine.</p>
            <div className="mt-8 flex flex-wrap items-center gap-4">
              <a href={destination} className={linkStyle}>{action}<ArrowRight aria-hidden="true" className="size-4" /></a>
              <a href="#how-it-works" className="rounded px-1 py-3 text-sm font-medium underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring">See how it works</a>
            </div>
            <p className="mt-5 flex items-center gap-2 text-sm text-muted-foreground"><ShieldCheck aria-hidden="true" className="size-4 text-primary" />Every email starts with your approval.</p>
          </div>
          <div className="relative">
            <div aria-hidden="true" className="absolute -inset-4 rounded-[2.5rem] bg-primary/5 sm:-inset-6" />
            <figure className="relative overflow-hidden rounded-2xl border bg-card shadow-xl shadow-primary/5">
              <figcaption className="flex items-center justify-between border-b px-5 py-4 text-xs sm:px-7">
                <span className="flex items-center gap-2 font-medium"><PenLine aria-hidden="true" className="size-4 text-primary" />Your next conversation</span>
                <span className="text-muted-foreground">Example draft</span>
              </figcaption>
              <div className="p-5 sm:p-7">
                <div className="space-y-3 border-b pb-5 text-sm">
                  <p className="flex gap-4"><span className="w-12 text-muted-foreground">To</span><span>Alex at the team</span></p>
                  <p className="flex gap-4"><span className="w-12 text-muted-foreground">Subject</span><span className="font-medium">A small introduction</span></p>
                </div>
                <div className="space-y-4 py-6 text-sm leading-7">
                  <p>Hi Alex,</p>
                  <p>I’m a software engineer who enjoys turning complex problems into useful tools. I’d love to learn more about your team and where I could contribute.</p>
                  <p>Would you be open to a short conversation?</p>
                  <p>Thanks,<br />Sam</p>
                </div>
                <div className="flex flex-wrap items-center justify-between gap-3 border-t pt-5">
                  <span className="flex items-center gap-2 text-xs text-muted-foreground"><Check aria-hidden="true" className="size-4 text-primary" />Written with AI. Refined by you.</span>
                  <span className="rounded-full bg-primary/10 px-3 py-1.5 text-xs font-medium text-primary">Ready for your review</span>
                </div>
              </div>
            </figure>
          </div>
        </section>
        <section id="how-it-works" className="scroll-mt-8 border-y bg-card/60">
          <div className="mx-auto max-w-6xl px-6 py-16 sm:py-20">
            <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-end">
              <div><p className="text-xs font-semibold uppercase tracking-[0.18em] text-primary">From context to conversation</p><h2 className="mt-3 text-3xl font-semibold tracking-tight sm:text-4xl">Your voice. A little head start.</h2></div>
              <p className="max-w-xs text-sm leading-relaxed text-muted-foreground">One place to keep your contacts, shape your message, and follow its delivery.</p>
            </div>
            <ol className="mt-12 grid gap-10 md:grid-cols-3 md:gap-12">
              {[
                { icon: FileText, title: 'Start with your story', text: 'Add a contact and paste your résumé text. Give Sefir the context for a relevant introduction.' },
                { icon: PenLine, title: 'Make every word yours', text: 'Get a starting draft from AI or write it yourself. Edit the recipient, subject, and message with a full text editor.' },
                { icon: Send, title: 'Review, approve, send', text: 'Approve the exact version you want to send through Gmail. Come back to check its delivery status.' },
              ].map(({ icon: Icon, title, text }, index) => (
                <li key={title} className="border-t pt-6">
                  <div className="mb-5 flex items-center justify-between"><Icon aria-hidden="true" className="size-6 text-primary" /><span className="font-mono text-xs text-muted-foreground">0{index + 1}</span></div>
                  <h3 className="text-lg font-semibold">{title}</h3><p className="mt-3 text-sm leading-7 text-muted-foreground">{text}</p>
                </li>
              ))}
            </ol>
          </div>
        </section>
        <section id="self-host" aria-labelledby="self-host-title" className="mx-auto grid max-w-6xl scroll-mt-8 gap-10 px-6 py-16 sm:py-24 md:grid-cols-[1.2fr_1fr] md:items-center md:gap-20">
          <div>
            <Laptop aria-hidden="true" className="mb-5 size-8 text-primary" />
            <p className="text-xs font-semibold uppercase tracking-[0.18em] text-primary">Self-host Sefir</p>
            <h2 id="self-host-title" className="mt-3 text-3xl font-semibold tracking-tight sm:text-4xl">Your workspace, on your own machine.</h2>
            <p className="mt-5 max-w-lg leading-7 text-muted-foreground">Sefir runs locally with Docker Compose. Your contacts, saved drafts, and delivery history live in that installation. You can inspect and adapt the source under the Apache-2.0 license.</p>
            <p className="mt-4 max-w-lg leading-7 text-muted-foreground">Bring your own Cohere, Anthropic, OpenAI, or OpenRouter API key for AI drafts and connect your Gmail account for sending. Provider usage and hosting costs are yours to manage.</p>
            <div className="mt-6 flex flex-wrap items-center gap-4">
              <a href={setupGuideURL} className={linkStyle} target="_blank" rel="noreferrer">Read the setup guide<ArrowUpRight aria-hidden="true" className="size-4" /></a>
              <a href={repositoryURL} className="rounded px-1 py-3 text-sm font-medium underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring" target="_blank" rel="noreferrer">View source on GitHub</a>
            </div>
          </div>
          <div className="rounded-2xl border bg-card p-7 sm:p-9">
            <p className="mb-3 text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground">This instance</p>
            <h2 className="text-2xl font-semibold">{session.authenticated ? 'Welcome back.' : signupAvailable ? 'Create your account' : 'Existing accounts only'}</h2>
            <p className="mt-3 text-sm leading-6 text-muted-foreground">{session.authenticated ? 'Open your workspace to write, review, and follow your outreach.' : signupAvailable ? 'Registration is enabled on this instance. Create your private workspace with Google or a username and password.' : 'Registration is closed on this instance. Existing accounts can sign in with Google or their password. You can also run your own copy of Sefir.'}</p>
            <div className="mt-6 space-y-3">
              {!session.authenticated && signupAvailable && <GoogleSignIn mode="signup" available={session.google_available} />}
              <a href={session.authenticated ? '#drafts' : signupAvailable ? '#signup' : '#signin'} className={`${linkStyle} w-full`}>{session.authenticated ? 'Open your workspace' : signupAvailable ? 'Create your account' : 'Sign in to your account'}<ArrowRight aria-hidden="true" className="size-4" /></a>
            </div>
          </div>
        </section>
        <section aria-labelledby="data-title" className="border-t bg-card/60">
          <div className="mx-auto max-w-6xl px-6 py-16 sm:py-20">
            <h2 id="data-title" className="text-3xl font-semibold tracking-tight">Know where your data goes.</h2>
            <div className="mt-8 grid gap-8 md:grid-cols-3">
              <div><h3 className="font-semibold">Saved on your instance</h3><p className="mt-3 text-sm leading-7 text-muted-foreground">Your account, contacts, draft versions, delivery records, and Gmail connection are stored on the instance you use.</p></div>
              <div><h3 className="font-semibold">AI when you choose it</h3><p className="mt-3 text-sm leading-7 text-muted-foreground">Generating a draft sends the résumé text and contact details you provide to your selected AI provider. Choose from the providers enabled on your installation, or write an email yourself without a model call.</p></div>
              <div><h3 className="font-semibold">Your approval comes first</h3><p className="mt-3 text-sm leading-7 text-muted-foreground">Review the recipient and exact draft version before sending through Gmail. Google sign-in and permission to send email are separate connections.</p></div>
            </div>
          </div>
        </section>
      </main>
      <footer className="border-t">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-3 px-6 py-6 text-sm text-muted-foreground"><span className="font-semibold text-foreground">Sefir</span><p>Open source · Apache-2.0</p><a href="#home" className="underline-offset-4 hover:underline">Back to top ↑</a></div>
      </footer>
    </div>
  )
}
