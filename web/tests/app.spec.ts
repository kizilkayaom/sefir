import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

const contact = {
  id: '33333333-3333-4333-8333-333333333333',
  name: 'Alex Example',
  email: 'alex@example.com',
  organization: 'Example team',
  role: 'Engineer',
  context: '',
  status: 'confirmed',
  created_at: '2026-10-28T09:00:00Z',
}
const draft = {
  draft_id: '11111111-1111-4111-8111-111111111111',
  version: 1,
  contact_id: contact.id,
  recipient_email: contact.email,
  subject: 'An introduction',
  body: 'Hi Alex,\n\nThis is a fictional test email.\n\nThanks.',
  body_html: '',
  content_hash: 'a'.repeat(64),
  created_at: '2026-10-28T09:00:00Z',
}
const missingRecipient = { ...draft, draft_id: '22222222-2222-4222-8222-222222222222', subject: 'Needs a recipient', recipient_email: null }
const delivery = {
  outbox_id: '44444444-4444-4444-8444-444444444444',
  draft_id: draft.draft_id,
  version: 1,
  subject: draft.subject,
  recipient_email: draft.recipient_email,
  approved_by: 'Owner',
  state: 'sent',
  attempts: 1,
  gmail_message_id: 'gmail-test',
  last_error: null,
  updated_at: '2026-10-28T09:00:00Z',
}
const writingProfile = {
  profile_id: '55555555-5555-4555-8555-555555555555',
  name: 'Personal introduction',
  language: 'en',
  content: 'Keep the opening brief.\n\n  Preserve this indentation.\n---\n<img src="invalid" onerror="alert(1)">\n' + 'word'.repeat(80),
  format_version: 1,
  revision: 3,
  created_at: '2026-11-12T09:00:00Z',
  updated_at: '2026-11-12T09:00:00Z',
}
const uploadedWritingProfile = {
  ...writingProfile,
  profile_id: '66666666-6666-4666-8666-666666666666',
  name: 'Warm and direct',
  content: 'Use a friendly greeting and short sentences.\n',
  revision: 1,
}
const writingProfileFile = {
  name: 'warm.MD',
  mimeType: 'text/markdown',
  buffer: Buffer.from(`---\nformat_version: 1\nname: Warm and direct\nlanguage: en\n---\n${uploadedWritingProfile.content}`),
}

async function mockAPI(page: Page) {
  const state = {
    authenticated: true,
    deletionError: false,
    deletionRequests: [] as { confirmation: string }[],
    deletionWait: null as Promise<void> | null,
    signupAvailable: false as boolean | undefined,
    setupError: false,
    googleAvailable: true,
    passwordAvailable: true,
    googleLinked: false,
    googleError: false,
    googleAccountExists: true,
    googleRequests: [] as string[],
    defaultPreset: 'prof',
    accountError: false,
    accountWait: null as Promise<void> | null,
    presetRequests: [] as { preset_id: string }[],
    presetSaveError: false,
    presetSaveWait: null as Promise<void> | null,
    writingProfiles: [] as typeof writingProfile[],
    writingProfilesError: false,
    writingProfilesWait: null as Promise<void> | null,
    writingProfileRequests: [] as { limit: number, offset: number }[],
    writingProfileUploads: [] as { name: string, content: string }[],
    writingProfileUploadError: null as { status: number, error: string } | null,
    writingProfileUploadWait: null as Promise<void> | null,
    writingProfileDeletes: [] as string[],
    writingProfileDeleteError: false,
    writingProfileDeleteWait: null as Promise<void> | null,
    setupRequests: [] as Record<string, string>[],
    expired: false,
    drafts: [draft, missingRecipient],
    contacts: [contact],
    approved: false,
    draftError: false,
    status: 'NOT_STARTED',
    failure: '',
    approvalRequests: [] as { content_hash: string }[],
    savedContact: null as Record<string, unknown> | null,
    generated: false,
    generationRequests: [] as Record<string, string>[],
    generationError: false,
    providerError: false,
    aiOperator: false,
    providerSaveError: false,
    providerModelError: false,
    modelRequests: [] as { provider: string, method: string }[],
    providerModels: {
      cohere: [{ id: 'command-a-plus-05-2026', name: 'command-a-plus-05-2026' }],
      anthropic: [{ id: 'claude-sonnet-5-5', name: 'Claude Sonnet 5.5' }, { id: 'claude-opus-5-5', name: 'Claude Opus 5.5' }],
      openai: [{ id: 'gpt-6-luna', name: 'gpt-6-luna' }, { id: 'gpt-6.1-sol', name: 'gpt-6.1-sol' }],
      openrouter: [{ id: 'openai/gpt-6-luna', name: 'OpenAI: GPT-6 Luna' }, { id: 'vendor/model:free', name: 'Free text model' }],
    } as Record<string, { id: string, name: string }[]>,
    providerUpdates: [] as { provider: string, api_key: string, model: string }[],
    defaultProvider: 'cohere' as string | null,
    providers: [
      { id: 'cohere', name: 'Cohere', model: 'command-a-plus-05-2026' },
      { id: 'anthropic', name: 'Anthropic (Claude)', model: 'claude-sonnet-5-5' },
      { id: 'openai', name: 'OpenAI (GPT)', model: 'gpt-6-luna' },
      { id: 'openrouter', name: 'OpenRouter', model: 'openai/gpt-6-luna' },
    ],
    manualRequest: null as Record<string, string> | null,
    editRequests: [] as Record<string, string>[],
    editError: false,
  }
  await page.route('**/api/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    const reply = (body: unknown, status = 200) => route.fulfill({ status, json: body })
    if (path === '/api/session') return reply({ authenticated: state.authenticated, signup_available: state.signupAvailable, google_available: state.googleAvailable, password_available: state.passwordAvailable, owner: 'Owner', csrf_token: 'csrf-test' })
    if (path === '/api/auth/google') {
      const mode = request.postDataJSON().mode
      state.googleRequests.push(mode)
      if (mode === 'signup' && !state.signupAvailable) return reply({ error: 'Registration is closed on this instance. Existing accounts can still sign in.' }, 403)
      if (state.googleError) return reply({ error: 'Google sign-in is temporarily unavailable. Please try again.' }, 503)
      if (!state.signupAvailable && !state.googleAccountExists) return reply({ url: 'http://127.0.0.1:4173/?auth=signup_closed#signin' })
      state.authenticated = true
      return reply({ url: 'http://127.0.0.1:4173/?google-return=1#drafts' })
    }
    if (path === '/api/signup') {
      state.setupRequests.push(request.postDataJSON())
      if (!state.signupAvailable) return reply({ error: 'Registration is closed on this instance. Existing accounts can still sign in.' }, 403)
      if (state.setupError) return reply({ error: 'Could not save your account. Please try again.' }, 503)
      state.authenticated = true
      return reply({ authenticated: true, owner: 'Owner', csrf_token: 'csrf-test' })
    }
    if (path === '/api/login') {
      const input = request.postDataJSON()
      if (input.username !== 'Owner' || input.password !== 'letter1!') return reply({ error: 'Incorrect username or password' }, 401)
      state.authenticated = true
      return reply({ authenticated: true, owner: 'Owner', csrf_token: 'csrf-test' })
    }
    if (state.expired || !state.authenticated) return reply({ error: 'Sign in to continue' }, 401)
    if (method !== 'GET') expect(request.headers()['x-csrf-token']).toBe('csrf-test')
    if (path === '/api/settings/account' && method === 'DELETE') {
      state.deletionRequests.push(request.postDataJSON())
      if (state.deletionWait) await state.deletionWait
      if (state.deletionError) return reply({ error: 'Database operation failed. Please try again.' }, 503)
      state.authenticated = false
      return reply({ status: 'deletion_requested' }, 202)
    }
    if (path === '/api/settings/account') {
      if (state.accountWait) await state.accountWait
      if (state.accountError) return reply({ error: 'Could not load account settings.' }, 503)
      return reply({ google_available: state.googleAvailable, google_linked: state.googleLinked, google_email: state.googleLinked ? 'owner@example.com' : '', default_preset_id: state.defaultPreset })
    }
    if (path === '/api/settings/account/style' && method === 'PUT') {
      const input = request.postDataJSON()
      state.presetRequests.push(input)
      if (state.presetSaveWait) await state.presetSaveWait
      if (state.presetSaveError) return reply({ error: 'Database operation failed. Please try again.' }, 503)
      state.defaultPreset = input.preset_id
      return reply({ default_preset_id: state.defaultPreset })
    }
    if (path === '/api/settings/writing-styles' && method === 'POST') {
      const contentType = request.headers()['content-type']
      expect(contentType).toMatch(/^multipart\/form-data; boundary=/)
      const data = await new Response(request.postDataBuffer(), { headers: { 'Content-Type': contentType } }).formData()
      expect([...data.keys()]).toEqual(['profile'])
      const file = data.get('profile')
      if (!(file instanceof File)) throw new Error('Expected a profile file')
      state.writingProfileUploads.push({ name: file.name, content: await file.text() })
      if (state.writingProfileUploadWait) await state.writingProfileUploadWait
      if (state.writingProfileUploadError) return reply({ error: state.writingProfileUploadError.error }, state.writingProfileUploadError.status)
      state.writingProfiles.unshift(uploadedWritingProfile)
      return reply(uploadedWritingProfile, 201)
    }
    if (path.startsWith('/api/settings/writing-styles/') && method === 'DELETE') {
      const id = decodeURIComponent(path.split('/').at(-1)!)
      state.writingProfileDeletes.push(id)
      if (state.writingProfileDeleteWait) await state.writingProfileDeleteWait
      if (state.writingProfileDeleteError) return reply({ error: 'Database operation failed. Please try again.' }, 503)
      if (!state.writingProfiles.some((profile) => profile.profile_id === id)) return reply({ error: 'Record not found' }, 404)
      state.writingProfiles = state.writingProfiles.filter((profile) => profile.profile_id !== id)
      return reply({ status: 'deleted' })
    }
    if (path === '/api/settings/writing-styles' && method === 'GET') {
      const params = new URL(request.url()).searchParams
      const limit = Number(params.get('limit'))
      const offset = Number(params.get('offset'))
      state.writingProfileRequests.push({ limit, offset })
      if (state.writingProfilesWait) await state.writingProfilesWait
      if (state.writingProfilesError) return reply({ error: 'Could not load writing profiles.' }, 503)
      const items = state.writingProfiles.slice(offset, offset + limit)
      return reply({ items, has_more: offset + limit < state.writingProfiles.length, offset, next_offset: offset + items.length })
    }
    if (path === '/api/settings/ai') {
      if (state.providerError) return reply({ error: 'Could not load AI providers.' }, 503)
      const options = [
        { id: 'cohere', name: 'Cohere', model: 'command-a-plus-05-2026' },
        { id: 'anthropic', name: 'Anthropic (Claude)', model: 'claude-sonnet-5-5' },
        { id: 'openai', name: 'OpenAI (GPT)', model: 'gpt-6-luna' },
        { id: 'openrouter', name: 'OpenRouter', model: 'openai/gpt-6-luna' },
      ].map((provider) => ({ ...provider, ...state.providers.find((item) => item.id === provider.id), configured: state.providers.some((item) => item.id === provider.id) }))
      return reply({ default_provider: state.defaultProvider, providers: state.providers, can_manage: state.aiOperator, options: state.aiOperator ? options : undefined })
    }
    if (path.startsWith('/api/settings/ai/') && path.endsWith('/models')) {
      if (!state.aiOperator) return reply({ error: 'Only the installation operator can change shared AI settings' }, 403)
      const provider = path.split('/')[4]
      state.modelRequests.push({ provider, method })
      if (state.providerModelError) return reply({ error: 'Could not fetch models. Try again or enter a model ID manually.' }, 503)
      return reply({ models: state.providerModels[provider] ?? [] })
    }
    if (path.startsWith('/api/settings/ai/')) {
      if (!state.aiOperator) return reply({ error: 'Only the installation operator can change shared AI settings' }, 403)
      if (state.providerSaveError) return reply({ error: 'Could not save AI settings.' }, 503)
      const provider = path.split('/').at(-1)!
      if (provider === 'default') {
        state.defaultProvider = request.postDataJSON().provider || state.providers[0]?.id || null
      } else if (method === 'DELETE') {
        state.providers = state.providers.filter((item) => item.id !== provider)
        if (state.defaultProvider === provider) state.defaultProvider = state.providers[0]?.id || null
      } else {
        const input = request.postDataJSON()
        state.providerUpdates.push({ provider, ...input })
        const name = { cohere: 'Cohere', anthropic: 'Anthropic (Claude)', openai: 'OpenAI (GPT)', openrouter: 'OpenRouter' }[provider]!
        state.providers = [...state.providers.filter((item) => item.id !== provider), { id: provider, name, model: input.model }]
        state.defaultProvider ??= provider
      }
      return reply({ saved: true })
    }
    if (path === '/api/settings/account/google') {
      state.googleRequests.push('link')
      state.googleLinked = true
      return reply({ url: 'http://127.0.0.1:4173/?auth=linked#settings' })
    }
    if (path === '/api/logout') {
      state.authenticated = false
      return reply({ authenticated: false })
    }
    if (path === '/api/drafts' && method === 'GET') {
      if (state.draftError) return reply({ error: 'Database unavailable' }, 503)
      return reply({ items: state.drafts, has_more: false, offset: 0, next_offset: state.drafts.length })
    }
    if (path.endsWith('/status')) {
      return reply({
        workflow: { status: state.approved ? 'COMPLETED' : state.status, failure: state.failure || null },
        delivery: state.approved ? delivery : null,
      })
    }
    if (path.endsWith('/approve')) {
      state.approvalRequests.push(request.postDataJSON())
      state.approved = true
      return reply({ workflow: { status: 'RUNNING' } }, 202)
    }
    if (path === '/api/drafts' && method === 'POST') {
      expect(request.postDataJSON().contact_id).toBe(contact.id)
      expect(request.postDataJSON().cv_text).toContain('Fictional resume')
      state.generationRequests.push(request.postDataJSON())
      if (state.generationError) return reply({ error: 'Draft generation failed. Check the selected AI provider.' }, 502)
      state.generated = true
      return reply(draft, 201)
    }
    if (path === '/api/drafts/manual') {
      const input = request.postDataJSON()
      state.manualRequest = input
      const saved = { ...draft, draft_id: '55555555-5555-4555-8555-555555555555', ...input, content_hash: 'b'.repeat(64) }
      state.drafts.unshift(saved)
      return reply(saved, 201)
    }
    if (path.endsWith('/edit')) {
      const input = request.postDataJSON()
      state.editRequests.push(input)
      if (state.editError) return reply({ error: 'A newer version exists. Copy your edits before refreshing.' }, 409)
      const original = state.drafts.find((item) => path === `/api/drafts/${item.draft_id}/versions/${item.version}/edit`)
      if (!original) return reply({ error: 'Record not found' }, 404)
      const saved = { ...original, ...input, version: original.version + 1, content_hash: 'b'.repeat(64) }
      state.drafts.unshift(saved)
      return reply(saved, 201)
    }
    if (path.startsWith('/api/drafts/')) {
      const found = state.drafts.find((item) => path === `/api/drafts/${item.draft_id}/versions/${item.version}`)
      return found ? reply(found) : reply({ error: 'Record not found' }, 404)
    }
    if (path.startsWith('/api/contacts') && method !== 'GET') {
      state.savedContact = request.postDataJSON()
      return reply({ ...contact, ...state.savedContact })
    }
    if (path === '/api/contacts') return reply({ items: state.contacts, has_more: false, offset: 0, next_offset: state.contacts.length })
    if (path === '/api/activity') return reply({ items: state.approved ? [delivery] : [], has_more: false, offset: 0, next_offset: 0 })
    if (path === '/api/settings/gmail') return reply({ connected: true, email: 'owner@example.com' })
    if (path === '/api/settings/gmail/connect') return reply({ url: 'http://127.0.0.1:4173/?gmail=connected#settings' })
    return reply({ error: 'Unexpected test request' }, 500)
  })
  return state
}

test('owner can sign in and sign out', async ({ page }) => {
  const state = await mockAPI(page)
  state.authenticated = false
  await page.goto('/')
  await page.getByRole('link', { name: 'Sign in', exact: true }).click()
  await page.getByLabel('Username', { exact: true }).fill('Owner')
  await expect(page.getByLabel('Confirm password', { exact: true })).toHaveCount(0)
  await page.getByLabel('Password', { exact: true }).fill('wrong')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('Incorrect username or password')
  await page.getByLabel('Password', { exact: true }).fill('letter1!')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Drafts', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Account: Owner' }).click()
  await page.getByRole('menuitem', { name: 'Sign out' }).click()
  await expect(page.getByRole('heading', { name: 'A better first hello.' })).toBeVisible()
})

test('signup shows live requirements and requires matching passwords', async ({ page }) => {
  const state = await mockAPI(page)
  state.signupAvailable = true
  state.authenticated = false
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  await page.getByRole('link', { name: 'Create your account', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Create your account' })).toBeVisible()
  const create = page.getByRole('button', { name: 'Create account', exact: true })
  const username = page.getByLabel('Username', { exact: true })
  const password = page.getByLabel('Password', { exact: true })
  const confirmation = page.getByLabel('Confirm password', { exact: true })
  const rules = page.locator('#password-requirements li')
  await expect(create).toBeDisabled()
  await expect(rules).toHaveCount(4)
  for (const rule of await rules.all()) {
    await expect(rule).toHaveAttribute('data-met', 'false')
    await expect(rule).toHaveClass(/text-muted-foreground/)
  }
  await username.fill('a b')
  await expect(page.locator('#username-requirements li')).toHaveAttribute('data-met', 'false')
  await username.fill('Owner')
  await expect(page.locator('#username-requirements li')).toHaveAttribute('data-met', 'true')
  await password.fill('letter')
  await expect(rules.nth(0)).toHaveAttribute('data-met', 'false')
  await expect(rules.nth(1)).toHaveAttribute('data-met', 'true')
  await password.fill('letter1')
  await expect(rules.nth(2)).toHaveAttribute('data-met', 'true')
  await expect(rules.nth(3)).toHaveAttribute('data-met', 'false')
  await password.fill('letter1!')
  for (const rule of await rules.all()) {
    await expect(rule).toHaveAttribute('data-met', 'true')
    await expect(rule).toHaveClass(/text-emerald-700/)
  }
  await confirmation.fill('letter1?')
  await expect(create).toBeDisabled()
  await expect(confirmation).toHaveAttribute('aria-invalid', 'true')
  await confirmation.press('Enter')
  expect(state.setupRequests).toHaveLength(0)
  await confirmation.fill('letter1!')
  await expect(page.locator('#password-match li')).toHaveAttribute('data-met', 'true')
  await expect(create).toBeEnabled()
  await password.fill('letter1')
  await expect(create).toBeDisabled()
  await expect(page.locator('#password-match li')).toHaveAttribute('data-met', 'false')
  await password.fill('letter1!')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await create.click()
  await expect(page.getByRole('heading', { name: 'Drafts', exact: true })).toBeVisible()
  expect(state.setupRequests).toEqual([{ username: 'Owner', password: 'letter1!', confirm_password: 'letter1!' }])
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Drafts', exact: true })).toBeVisible()
})

test('account setup keeps inputs available after a server error', async ({ page }) => {
  const state = await mockAPI(page)
  state.signupAvailable = true
  state.authenticated = false
  state.setupError = true
  await page.goto('/#signup')
  await page.getByLabel('Username', { exact: true }).fill('Owner')
  await page.getByLabel('Password', { exact: true }).fill('letter1!')
  await page.getByLabel('Confirm password', { exact: true }).fill('letter1!')
  await page.getByRole('button', { name: 'Create account', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('Could not save your account. Please try again.')
  await expect(page.getByLabel('Confirm password', { exact: true })).toHaveValue('letter1!')
  await expect(page.getByRole('button', { name: 'Create account', exact: true })).toBeEnabled()
  state.setupError = false
  await page.getByRole('button', { name: 'Create account', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Drafts', exact: true })).toBeVisible()
})

test('approval requires review and sends the exact content hash once', async ({ page }) => {
  const state = await mockAPI(page)
  await page.goto('/')
  await page.getByRole('button', { name: /An introduction/ }).click()
  const approve = page.getByRole('button', { name: 'Approve and send', exact: true })
  await expect(approve).toBeDisabled()
  await expect(page.getByText(draft.body)).toHaveCSS('white-space', 'pre-wrap')
  await page.getByRole('checkbox', { name: /I reviewed version 1/ }).check()
  await approve.click()
  await expect(page.getByText('Delivery:', { exact: false })).toContainText('sent')
  await expect(approve).toBeDisabled()
  expect(state.approvalRequests).toEqual([{ content_hash: draft.content_hash }])
  await page.getByRole('link', { name: 'Activity', exact: true }).click()
  await expect(page.getByText('Delivery:', { exact: false })).toContainText('sent')
})

test('missing recipient and failed workflows cannot be approved', async ({ page }) => {
  const state = await mockAPI(page)
  await page.goto('/')
  await page.getByRole('button', { name: /Needs a recipient/ }).click()
  await expect(page.getByRole('button', { name: 'Approve and send' })).toBeDisabled()
  await expect(page.getByText('Edit this draft and enter a recipient before approving.')).toBeVisible()
  state.status = 'FAILED'
  state.failure = 'Delivery was not confirmed'
  await page.getByRole('button', { name: /An introduction/ }).click()
  await expect(page.getByText('Delivery was not confirmed')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Approve and send' })).toBeDisabled()
  expect(state.approvalRequests).toHaveLength(0)
})

test('mobile selection shows a reading pane and back restores the list', async ({ page }) => {
  await mockAPI(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  await page.getByRole('button', { name: /An introduction/ }).focus()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('heading', { name: draft.subject })).toBeVisible()
  await expect(page.getByRole('button', { name: /Needs a recipient/ })).toBeHidden()
  await expect(page.locator('body')).toHaveJSProperty('scrollWidth', 390)
  await page.getByRole('button', { name: 'Back to drafts' }).click()
  await expect(page.getByRole('button', { name: /Needs a recipient/ })).toBeVisible()
})

test('errors can be retried and empty results are shown honestly', async ({ page }) => {
  const state = await mockAPI(page)
  state.draftError = true
  await page.goto('/')
  await expect(page.getByRole('alert')).toHaveText('Database unavailable')
  state.draftError = false
  state.drafts = []
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect(page.getByText('No drafts loaded')).toBeVisible()
})

test('contact changes and draft generation use the API', async ({ page }) => {
  const state = await mockAPI(page)
  await page.goto('/#contacts')
  await page.getByLabel('Name', { exact: true }).fill('New contact')
  await page.getByLabel('Email', { exact: true }).fill('new@example.com')
  await page.getByLabel('I have checked these contact details.').check()
  await page.getByRole('button', { name: 'Save contact' }).click()
  await expect(page.getByText('Contact saved.')).toBeVisible()
  expect(state.savedContact?.status).toBe('confirmed')
  await page.getByRole('button', { name: 'Draft email' }).click()
  await expect(page.getByLabel('Writing style')).toHaveValue('prof')
  await page.getByLabel('Résumé text').fill('Fictional resume for automated testing.')
  await page.getByRole('button', { name: 'Generate draft' }).click()
  await expect(page.getByRole('heading', { name: draft.subject })).toBeVisible()
  expect(state.generated).toBe(true)
  expect(state.generationRequests).toHaveLength(1)
  expect(state.generationRequests[0].provider).toBe('cohere')
  expect(state.generationRequests[0].preset_id).toBe('prof')
  expect(state.approvalRequests).toHaveLength(0)
})

for (const provider of ['anthropic', 'openai', 'openrouter']) {
  test(`draft generation uses the selected ${provider} provider`, async ({ page }) => {
    const state = await mockAPI(page)
    await page.goto('/#new')
    await page.getByLabel('Contact', { exact: true }).selectOption(contact.id)
    await page.getByLabel('AI provider').selectOption(provider)
    const choice = state.providers.find((item) => item.id === provider)!
    await expect(page.getByText(`Model: ${choice.model}`)).toBeVisible()
    await expect(page.getByText(`Generating this draft sends your résumé text and selected contact's details to ${choice.name}.`)).toBeVisible()
    if (provider === 'openrouter') {
      await expect(page.getByText('OpenRouter forwards this information to the provider serving your selected model.')).toBeVisible()
    }
    await page.getByLabel('Résumé text').fill('Fictional resume for automated testing.')
    if (provider === 'anthropic') {
      await page.setViewportSize({ width: 390, height: 844 })
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
      await page.screenshot({ path: test.info().outputPath('ai-provider-mobile.png'), fullPage: true })
    }
    await page.getByRole('button', { name: 'Generate draft' }).click()
    await expect(page.getByRole('heading', { name: draft.subject })).toBeVisible()
    expect(state.generationRequests).toHaveLength(1)
    expect(state.generationRequests[0].provider).toBe(provider)
    expect(state.approvalRequests).toHaveLength(0)
  })
}

test('an OpenAI-only installation uses its configured default', async ({ page }) => {
  const state = await mockAPI(page)
  state.providers = state.providers.filter((item) => item.id === 'openai')
  state.defaultProvider = 'openai'
  await page.goto('/#new')
  await expect(page.getByLabel('AI provider')).toHaveValue('openai')
  await expect(page.getByLabel('AI provider').locator('option')).toHaveCount(2)
  await page.getByLabel('Contact', { exact: true }).selectOption(contact.id)
  await page.getByLabel('Résumé text').fill('Fictional resume for automated testing.')
  await page.getByRole('button', { name: 'Generate draft' }).click()
  await expect(page.getByRole('heading', { name: draft.subject })).toBeVisible()
  expect(state.generationRequests[0].provider).toBe('openai')
})

test('draft generation waits for the saved writing style and retries account loading', async ({ page }) => {
  const state = await mockAPI(page)
  state.defaultPreset = 'formal'
  state.accountError = true
  let finishLoading!: () => void
  state.accountWait = new Promise<void>((resolve) => { finishLoading = resolve })
  await page.goto('/#new')
  try {
    await page.getByLabel('Contact', { exact: true }).selectOption(contact.id)
    await page.getByLabel('Résumé text').fill('Fictional resume for automated testing.')
    await expect(page.getByLabel('AI provider')).toHaveValue('cohere')
    await expect(page.getByText('Loading writing style…', { exact: true })).toBeVisible()
    await expect(page.getByLabel('Writing style')).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Generate draft' })).toBeDisabled()
    await page.locator('form').dispatchEvent('submit')
    expect(state.generationRequests).toHaveLength(0)
  } finally {
    finishLoading()
  }
  await expect(page.getByRole('alert')).toHaveText('Could not load account settings.')
  await expect(page.getByRole('button', { name: 'Generate draft' })).toBeDisabled()
  state.accountError = false
  await page.getByRole('button', { name: 'Retry writing style' }).click()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.getByLabel('Writing style')).toHaveValue('formal')
  await page.getByRole('button', { name: 'Generate draft' }).click()
  await expect(page.getByRole('heading', { name: draft.subject })).toBeVisible()
  expect(state.generationRequests).toHaveLength(1)
  expect(state.generationRequests[0].preset_id).toBe('formal')
  expect(state.presetRequests).toHaveLength(0)
})

test('manual drafting remains available without an AI provider or account settings', async ({ page }) => {
  const state = await mockAPI(page)
  state.providers = []
  state.defaultProvider = null
  state.accountError = true
  await page.goto('/#new')
  await expect(page.getByText('AI generation is not configured on this installation. You can still choose Write yourself.')).toBeVisible()
  await expect(page.getByRole('alert')).toHaveText('Could not load account settings.')
  await page.getByLabel('Contact', { exact: true }).selectOption(contact.id)
  await page.getByLabel('Résumé text').fill('Fictional resume for automated testing.')
  await expect(page.getByRole('button', { name: 'Generate draft' })).toBeDisabled()
  await page.getByRole('button', { name: 'Write yourself' }).click()
  await expect(page.getByLabel('Writing style')).toHaveCount(0)
  await expect(page.getByRole('alert')).toHaveCount(0)
  await page.getByLabel('Subject', { exact: true }).fill('A manual draft')
  await page.getByRole('textbox', { name: 'Email body' }).fill('A fictional introduction.')
  await page.getByRole('button', { name: 'Save draft', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'A manual draft' })).toBeVisible()
  expect(state.generationRequests).toHaveLength(0)
  expect(state.manualRequest?.subject).toBe('A manual draft')
  expect(state.manualRequest).not.toHaveProperty('preset_id')
})

test('AI configuration errors can be retried and generation failures preserve the choice', async ({ page }) => {
  const state = await mockAPI(page)
  state.defaultPreset = 'warm_prof'
  state.providerError = true
  await page.goto('/#new')
  await expect(page.getByRole('alert')).toHaveText('Could not load AI providers.')
  await expect(page.getByRole('button', { name: 'Generate draft' })).toBeDisabled()
  state.providerError = false
  await page.getByRole('button', { name: 'Retry AI providers' }).click()
  await page.getByLabel('AI provider').selectOption('anthropic')
  await expect(page.getByLabel('Writing style')).toHaveValue('warm_prof')
  await page.getByLabel('Writing style').selectOption('formal')
  await page.getByLabel('Contact', { exact: true }).selectOption(contact.id)
  await page.getByLabel('Résumé text').fill('Fictional resume for automated testing.')
  state.generationError = true
  await page.getByRole('button', { name: 'Generate draft' }).click()
  await expect(page.getByRole('alert')).toHaveText('Draft generation failed. Check the selected AI provider.')
  await expect(page.getByLabel('AI provider')).toHaveValue('anthropic')
  await expect(page.getByLabel('Writing style')).toHaveValue('formal')
  await expect(page.getByLabel('Résumé text')).toHaveValue('Fictional resume for automated testing.')
  expect(state.generationRequests).toHaveLength(1)
  expect(state.generationRequests[0].provider).toBe('anthropic')
  expect(state.generationRequests[0].preset_id).toBe('formal')
  state.generationError = false
  await page.getByRole('button', { name: 'Generate draft' }).click()
  await expect(page.getByRole('heading', { name: draft.subject })).toBeVisible()
  expect(state.generationRequests).toHaveLength(2)
  expect(state.generationRequests[1].provider).toBe('anthropic')
  expect(state.generationRequests[1].preset_id).toBe('formal')
  expect(state.defaultPreset).toBe('warm_prof')
  expect(state.presetRequests).toHaveLength(0)
  expect(state.approvalRequests).toHaveLength(0)
})

test('account deletion requires exact confirmation and clears the workspace', async ({ page }) => {
  const state = await mockAPI(page)
  let finishDeletion!: () => void
  state.deletionWait = new Promise<void>((resolve) => { finishDeletion = resolve })
  await page.goto('/#settings')
  const button = page.getByRole('button', { name: 'Permanently delete account' })
  const confirmation = page.getByLabel('Type DELETE to confirm')
  await expect(button).toBeDisabled()
  await confirmation.fill('delete')
  await expect(button).toBeDisabled()
  await confirmation.press('Enter')
  expect(state.deletionRequests).toHaveLength(0)
  await confirmation.fill('DELETE')
  await button.click()
  await expect(page.getByRole('button', { name: 'Requesting deletion…' })).toBeDisabled()
  await expect(confirmation).toBeDisabled()
  await expect.poll(() => state.deletionRequests.length).toBe(1)
  finishDeletion()
  await expect(page.getByRole('heading', { name: 'Account deletion requested' })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Main navigation' })).toHaveCount(0)
  expect(state.deletionRequests).toEqual([{ confirmation: 'DELETE' }])
  await page.getByRole('button', { name: 'Back to home' }).click()
  await page.goto('/#drafts')
  await expect(page.getByLabel('Password', { exact: true })).toBeVisible()
  await expect(page.getByText(draft.subject, { exact: true })).toHaveCount(0)
})

test('failed account deletion preserves the session and can be retried', async ({ page }) => {
  const state = await mockAPI(page)
  state.deletionError = true
  await page.goto('/#settings')
  await page.getByLabel('Type DELETE to confirm').fill('DELETE')
  const button = page.getByRole('button', { name: 'Permanently delete account' })
  await button.click()
  await expect(page.getByRole('alert')).toHaveText('Database operation failed. Please try again.')
  await expect(button).toBeEnabled()
  await expect(page.getByLabel('Type DELETE to confirm')).toHaveValue('DELETE')
  expect(state.authenticated).toBe(true)
  state.deletionError = false
  await button.click()
  await expect(page.getByRole('heading', { name: 'Account deletion requested' })).toBeVisible()
  expect(state.deletionRequests).toHaveLength(2)
})

test('settings shows the connected account and session expiry returns to login', async ({ page }) => {
  const state = await mockAPI(page)
  await page.goto('/#settings')
  await expect(page.getByText('owner@example.com')).toBeVisible()
  await page.getByRole('button', { name: 'Reconnect Gmail' }).click()
  await expect(page.getByText('Gmail authorization saved.')).toBeVisible()
  state.expired = true
  await page.getByRole('button', { name: 'Check connection' }).click()
  await expect(page.getByLabel('Password', { exact: true })).toBeVisible()
})

test('account writing style loads the saved preference and persists changes after refresh', async ({ page }) => {
  const state = await mockAPI(page)
  state.defaultPreset = 'warm_prof'
  let finishSave!: () => void
  state.presetSaveWait = new Promise<void>((resolve) => { finishSave = resolve })
  await page.goto('/#settings')
  const preset = page.getByLabel('Default writing style')
  await expect(preset).toHaveValue('warm_prof')
  await preset.selectOption('formal')
  await page.getByRole('button', { name: 'Save writing style' }).click()
  try {
    await expect(page.getByRole('button', { name: 'Saving…', exact: true })).toBeDisabled()
    await expect(preset).toBeDisabled()
    await expect.poll(() => state.presetRequests).toEqual([{ preset_id: 'formal' }])
    expect(state.defaultPreset).toBe('warm_prof')
  } finally {
    finishSave()
  }
  await expect(page.getByRole('status').filter({ hasText: 'Default writing style saved.' })).toBeVisible()
  await expect(preset).toBeEnabled()
  await expect(preset).toHaveValue('formal')
  await page.reload()
  await expect(preset).toHaveValue('formal')
  expect(state.presetRequests).toEqual([{ preset_id: 'formal' }])
})

test('a failed writing style save preserves the selection for retry', async ({ page }) => {
  const state = await mockAPI(page)
  state.presetSaveError = true
  await page.goto('/#settings')
  const preset = page.getByLabel('Default writing style')
  await expect(preset).toHaveValue('prof')
  await preset.selectOption('technical')
  await page.getByRole('button', { name: 'Save writing style' }).click()
  await expect(page.getByRole('alert')).toContainText('Database operation failed.')
  await expect(preset).toHaveValue('technical')
  await expect(preset).toBeEnabled()
  await expect(page.getByText('Default writing style saved.', { exact: true })).toHaveCount(0)
  expect(state.defaultPreset).toBe('prof')
  state.presetSaveError = false
  await page.getByRole('button', { name: 'Save writing style' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Default writing style saved.' })).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(preset).toHaveValue('technical')
  expect(state.presetRequests).toEqual([{ preset_id: 'technical' }, { preset_id: 'technical' }])
  expect(state.defaultPreset).toBe('technical')
})

test('custom writing profiles show metadata and a plain text preview on desktop and mobile', async ({ page }) => {
  const state = await mockAPI(page)
  state.writingProfiles = [writingProfile]
  await page.goto('/#settings')
  const profiles = page.getByRole('region', { name: 'Custom writing profiles' })
  await expect(profiles.getByRole('heading', { name: writingProfile.name })).toBeVisible()
  await expect(profiles.getByText('en · Revision 3')).toBeVisible()
  await expect(profiles.locator('pre')).toBeHidden()
  const preview = profiles.getByText('Preview writing style', { exact: true })
  await preview.focus()
  await page.keyboard.press('Enter')
  await expect(profiles.locator('pre')).toBeVisible()
  await expect(profiles.locator('pre')).toHaveText(writingProfile.content, { useInnerText: false })
  expect(await profiles.locator('pre').textContent()).toBe(writingProfile.content)
  await expect(profiles.locator('img')).toHaveCount(0)
  await profiles.screenshot({ path: test.info().outputPath('writing-profiles-desktop.png') })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await profiles.screenshot({ path: test.info().outputPath('writing-profiles-mobile.png') })
  await preview.click()
  await expect(profiles.locator('pre')).toBeHidden()
})

test('custom writing profiles support pagination and refreshing the current page', async ({ page }) => {
  const state = await mockAPI(page)
  state.writingProfiles = Array.from({ length: 26 }, (_, index) => ({
    ...writingProfile, profile_id: `profile-${index}`, name: `Writing profile ${index + 1}`,
  }))
  await page.goto('/#settings')
  const profiles = page.getByRole('region', { name: 'Custom writing profiles' })
  await expect(profiles.getByRole('listitem')).toHaveCount(25)
  await expect(profiles.getByRole('button', { name: 'Previous' })).toBeDisabled()
  await profiles.getByRole('button', { name: 'Next' }).click()
  await expect(profiles.getByText('Page 2', { exact: true })).toBeVisible()
  await expect(profiles.getByRole('listitem')).toHaveCount(1)
  await expect(profiles.getByRole('heading', { name: 'Writing profile 26', exact: true })).toBeVisible()
  await expect(profiles.getByRole('button', { name: 'Next' })).toBeDisabled()
  state.writingProfiles[25] = { ...state.writingProfiles[25], name: 'Updated writing profile', revision: 4 }
  await profiles.getByRole('button', { name: 'Refresh' }).click()
  await expect(profiles.getByRole('heading', { name: 'Updated writing profile' })).toBeVisible()
  await expect(profiles.getByText('en · Revision 4')).toBeVisible()
  await profiles.getByRole('button', { name: 'Previous' }).click()
  await expect(profiles.getByText('Page 1', { exact: true })).toBeVisible()
  await expect(profiles.getByRole('listitem')).toHaveCount(25)
  expect(state.writingProfileRequests).toEqual([
    { limit: 25, offset: 0 },
    { limit: 25, offset: 25 },
    { limit: 25, offset: 25 },
    { limit: 25, offset: 0 },
  ])
})

test('custom writing profiles distinguish loading and errors from an empty list and allow retry', async ({ page }) => {
  const state = await mockAPI(page)
  state.writingProfilesError = true
  let finishLoading!: () => void
  state.writingProfilesWait = new Promise<void>((resolve) => { finishLoading = resolve })
  await page.goto('/#settings')
  const profiles = page.getByRole('region', { name: 'Custom writing profiles' })
  try {
    await expect(profiles.getByRole('status')).toHaveText('Loading writing profiles…')
    await expect(profiles.getByText('No custom writing profiles yet.')).toHaveCount(0)
    for (const name of ['Previous', 'Next', 'Refresh']) {
      await expect(profiles.getByRole('button', { name, exact: true })).toBeDisabled()
    }
  } finally {
    finishLoading()
  }
  await expect(profiles.getByRole('alert')).toContainText('Could not load writing profiles.')
  await expect(profiles.getByRole('status')).toHaveCount(0)
  await expect(profiles.getByText('No custom writing profiles yet.')).toHaveCount(0)
  state.writingProfilesError = false
  await profiles.getByRole('button', { name: 'Retry' }).click()
  await expect(profiles.getByText('No custom writing profiles yet.')).toBeVisible()
  await expect(profiles.getByRole('alert')).toHaveCount(0)
  await expect(profiles.getByRole('listitem')).toHaveCount(0)
  await expect(profiles.getByRole('button', { name: 'Next' })).toBeDisabled()
})

test('uploading a writing profile sends the file and refreshes the first page', async ({ page }) => {
  const state = await mockAPI(page)
  let finishUpload!: () => void
  state.writingProfileUploadWait = new Promise<void>((resolve) => { finishUpload = resolve })
  await page.goto('/#settings')
  const profiles = page.getByRole('region', { name: 'Custom writing profiles' })
  const form = profiles.getByRole('form', { name: 'Upload writing profile' })
  const input = form.getByLabel('Markdown profile')
  await expect(profiles.getByText('No custom writing profiles yet.')).toBeVisible()
  await expect(form.getByRole('button', { name: 'Upload profile' })).toBeDisabled()
  await input.setInputFiles(writingProfileFile)
  await form.getByRole('button', { name: 'Upload profile' }).click()
  try {
    await expect(input).toBeDisabled()
    await expect(form.getByRole('button', { name: 'Uploading…' })).toBeDisabled()
    await expect(profiles.getByRole('button', { name: 'Refresh' })).toBeDisabled()
    await expect.poll(() => state.writingProfileUploads).toEqual([
      { name: writingProfileFile.name, content: writingProfileFile.buffer.toString() },
    ])
  } finally {
    finishUpload()
  }
  await expect(form.getByRole('status')).toHaveText('Writing profile uploaded.')
  await expect(profiles.getByRole('heading', { name: uploadedWritingProfile.name })).toBeVisible()
  await expect(profiles.getByText('No custom writing profiles yet.')).toHaveCount(0)
  await expect(input).toBeEnabled()
  await expect(input).toHaveValue('')
  await expect(form.getByRole('button', { name: 'Upload profile' })).toBeDisabled()
  expect(state.writingProfileRequests).toEqual([{ limit: 25, offset: 0 }, { limit: 25, offset: 0 }])
  await input.setInputFiles(writingProfileFile)
  await expect(form.getByRole('status')).toHaveCount(0)
  await expect(form.getByRole('button', { name: 'Upload profile' })).toBeEnabled()
  await input.setInputFiles([])
  await expect(form.getByRole('button', { name: 'Upload profile' })).toBeDisabled()
})

test('uploading a writing profile from another page returns to the newest profiles', async ({ page }) => {
  const state = await mockAPI(page)
  let finishUpload!: () => void
  state.writingProfileUploadWait = new Promise<void>((resolve) => { finishUpload = resolve })
  state.writingProfiles = Array.from({ length: 26 }, (_, index) => ({
    ...writingProfile, profile_id: `profile-${index}`, name: `Writing profile ${index + 1}`,
  }))
  await page.goto('/#settings')
  const profiles = page.getByRole('region', { name: 'Custom writing profiles' })
  await profiles.getByRole('button', { name: 'Next' }).click()
  await expect(profiles.getByRole('heading', { name: 'Writing profile 26' })).toBeVisible()
  await profiles.getByLabel('Markdown profile').setInputFiles(writingProfileFile)
  await profiles.getByRole('button', { name: 'Upload profile', exact: true }).click()
  try {
    await expect(profiles.getByRole('button', { name: 'Uploading…' })).toBeDisabled()
    await expect(profiles.getByRole('button', { name: 'Delete profile', exact: true })).toBeDisabled()
    await expect(profiles.getByRole('button', { name: 'Previous' })).toBeDisabled()
    expect(state.writingProfileDeletes).toHaveLength(0)
  } finally {
    finishUpload()
  }
  await expect(profiles.getByText('Page 1', { exact: true })).toBeVisible()
  await expect(profiles.getByRole('listitem').first()).toContainText(uploadedWritingProfile.name)
  await expect(profiles.getByRole('listitem')).toHaveCount(25)
  expect(state.writingProfileRequests).toEqual([
    { limit: 25, offset: 0 }, { limit: 25, offset: 25 }, { limit: 25, offset: 0 },
  ])
})

test('writing profile uploads reject invalid files before making a request', async ({ page }) => {
  const state = await mockAPI(page)
  await page.goto('/#settings')
  const form = page.getByRole('form', { name: 'Upload writing profile' })
  const input = form.getByLabel('Markdown profile')
  const cases = [
    { name: 'profile.txt', buffer: writingProfileFile.buffer, error: 'Choose a Markdown (.md) file.' },
    { name: 'empty.md', buffer: Buffer.alloc(0), error: 'Profile file must not be empty.' },
    { name: 'large.md', buffer: Buffer.alloc(32 * 1024 + 1, 'a'), error: 'Profile file must not exceed 32 KiB.' },
  ]
  for (const file of cases) {
    await input.setInputFiles({ name: file.name, mimeType: 'text/markdown', buffer: file.buffer })
    await expect(form.getByRole('alert')).toHaveCount(0)
    await form.getByRole('button', { name: 'Upload profile' }).click()
    await expect(form.getByRole('alert')).toHaveText(file.error)
    await expect(input).toBeEnabled()
  }
  expect(state.writingProfileUploads).toHaveLength(0)
  await input.setInputFiles(writingProfileFile)
  await expect(form.getByRole('alert')).toHaveCount(0)
})

for (const failure of [
  { status: 422, error: 'profile frontmatter must include a name' },
  { status: 503, error: 'Database operation failed. Please try again.' },
]) {
  test(`writing profile upload failure ${failure.status} preserves the selected file for retry`, async ({ page }) => {
    const state = await mockAPI(page)
    state.writingProfiles = [writingProfile]
    state.writingProfileUploadError = failure
    await page.goto('/#settings')
    const profiles = page.getByRole('region', { name: 'Custom writing profiles' })
    const form = profiles.getByRole('form', { name: 'Upload writing profile' })
    const input = form.getByLabel('Markdown profile')
    await input.setInputFiles(writingProfileFile)
    await form.getByRole('button', { name: 'Upload profile' }).click()
    await expect(form.getByRole('alert')).toHaveText(failure.error)
    await expect(form.getByRole('status')).toHaveCount(0)
    await expect(input).toHaveValue(/warm\.MD$/)
    await expect(input).toBeEnabled()
    await expect(profiles.getByRole('heading', { name: writingProfile.name })).toBeVisible()
    state.writingProfileUploadError = null
    await form.getByRole('button', { name: 'Upload profile' }).click()
    await expect(form.getByRole('status')).toHaveText('Writing profile uploaded.')
    await expect(form.getByRole('alert')).toHaveCount(0)
    await expect(input).toHaveValue('')
    await expect(profiles.getByRole('heading', { name: uploadedWritingProfile.name })).toBeVisible()
    expect(state.writingProfileUploads).toEqual([
      { name: writingProfileFile.name, content: writingProfileFile.buffer.toString() },
      { name: writingProfileFile.name, content: writingProfileFile.buffer.toString() },
    ])
  })
}

test('deleting a writing profile requires confirmation and disables controls until it finishes', async ({ page }) => {
  const state = await mockAPI(page)
  state.writingProfiles = [writingProfile, uploadedWritingProfile]
  let finishDelete!: () => void
  state.writingProfileDeleteWait = new Promise<void>((resolve) => { finishDelete = resolve })
  await page.goto('/#settings')
  const profiles = page.getByRole('region', { name: 'Custom writing profiles' })
  const target = profiles.getByRole('listitem').filter({ has: page.getByRole('heading', { name: writingProfile.name }) })
  const other = profiles.getByRole('listitem').filter({ has: page.getByRole('heading', { name: uploadedWritingProfile.name }) })
  const input = profiles.getByLabel('Markdown profile')
  await input.setInputFiles(writingProfileFile)
  page.once('dialog', async (dialog) => {
    expect(dialog.type()).toBe('confirm')
    expect(dialog.message()).toBe(`Delete writing profile "${writingProfile.name}"? This cannot be undone.`)
    await dialog.dismiss()
  })
  await target.getByRole('button', { name: 'Delete profile' }).click()
  await expect(target).toBeVisible()
  expect(state.writingProfileDeletes).toHaveLength(0)
  page.once('dialog', (dialog) => dialog.accept())
  await target.getByRole('button', { name: 'Delete profile' }).click()
  try {
    await expect(target.getByRole('button', { name: 'Deleting…' })).toBeDisabled()
    await expect(other.getByRole('button', { name: 'Delete profile' })).toBeDisabled()
    await expect(input).toBeDisabled()
    await expect(profiles.getByRole('button', { name: 'Upload profile', exact: true })).toBeDisabled()
    await expect(profiles.getByRole('button', { name: 'Refresh' })).toBeDisabled()
    await expect.poll(() => state.writingProfileDeletes).toEqual([writingProfile.profile_id])
  } finally {
    finishDelete()
  }
  await expect(profiles.getByRole('status').filter({ hasText: 'Writing profile deleted.' })).toBeVisible()
  await expect(target).toHaveCount(0)
  await expect(other).toBeVisible()
  await expect(input).toBeEnabled()
  await expect(input).toHaveValue(/warm\.MD$/)
  await expect(profiles.getByRole('button', { name: 'Upload profile', exact: true })).toBeEnabled()
  await expect(other.getByRole('button', { name: 'Delete profile' })).toBeEnabled()
  expect(state.writingProfileRequests).toEqual([{ limit: 25, offset: 0 }, { limit: 25, offset: 0 }])
  expect(state.writingProfileUploads).toHaveLength(0)
  await page.reload()
  await expect(profiles.getByRole('listitem')).toHaveCount(1)
  await expect(other).toBeVisible()
  await expect(target).toHaveCount(0)
})

test('deleting the last writing profile on a later page returns to the previous page', async ({ page }) => {
  const state = await mockAPI(page)
  state.writingProfiles = Array.from({ length: 26 }, (_, index) => ({
    ...writingProfile, profile_id: `00000000-0000-4000-8000-${String(index + 1).padStart(12, '0')}`, name: `Writing profile ${index + 1}`,
  }))
  const lastID = state.writingProfiles[25].profile_id
  await page.goto('/#settings')
  const profiles = page.getByRole('region', { name: 'Custom writing profiles' })
  await profiles.getByRole('button', { name: 'Next' }).click()
  await expect(profiles.getByRole('heading', { name: 'Writing profile 26', exact: true })).toBeVisible()
  page.once('dialog', (dialog) => dialog.accept())
  await profiles.getByRole('button', { name: 'Delete profile' }).click()
  await expect(profiles.getByRole('status').filter({ hasText: 'Writing profile deleted.' })).toBeVisible()
  await expect(profiles.getByText('Page 1', { exact: true })).toBeVisible()
  await expect(profiles.getByRole('listitem')).toHaveCount(25)
  await expect(profiles.getByRole('heading', { name: 'Writing profile 26', exact: true })).toHaveCount(0)
  await expect(profiles.getByRole('button', { name: 'Next' })).toBeDisabled()
  await expect(profiles.getByRole('button', { name: 'Previous' })).toBeDisabled()
  expect(state.writingProfileDeletes).toEqual([lastID])
  expect(state.writingProfileRequests).toEqual([
    { limit: 25, offset: 0 }, { limit: 25, offset: 25 }, { limit: 25, offset: 0 },
  ])
})

test('failed writing profile deletion preserves the list and can be retried', async ({ page }) => {
  const state = await mockAPI(page)
  state.writingProfiles = [writingProfile]
  state.writingProfileDeleteError = true
  await page.goto('/#settings')
  const profiles = page.getByRole('region', { name: 'Custom writing profiles' })
  page.on('dialog', (dialog) => dialog.accept())
  await profiles.getByRole('button', { name: 'Delete profile' }).click()
  await expect(profiles.getByRole('alert')).toHaveText('Database operation failed. Please try again.')
  await expect(profiles.getByRole('heading', { name: writingProfile.name })).toBeVisible()
  await expect(profiles.getByRole('status')).toHaveCount(0)
  await expect(profiles.getByRole('form', { name: 'Upload writing profile' }).getByRole('alert')).toHaveCount(0)
  await expect(profiles.getByRole('button', { name: 'Delete profile' })).toBeEnabled()
  state.writingProfileDeleteError = false
  await profiles.getByRole('button', { name: 'Delete profile' }).click()
  await expect(profiles.getByRole('status').filter({ hasText: 'Writing profile deleted.' })).toBeVisible()
  await expect(profiles.getByRole('alert')).toHaveCount(0)
  await expect(profiles.getByText('No custom writing profiles yet.')).toBeVisible()
  await expect(profiles.getByRole('listitem')).toHaveCount(0)
  expect(state.writingProfileDeletes).toEqual([writingProfile.profile_id, writingProfile.profile_id])
  await profiles.getByLabel('Markdown profile').setInputFiles(writingProfileFile)
  await profiles.getByRole('button', { name: 'Upload profile', exact: true }).click()
  await expect(profiles.getByText('Writing profile deleted.', { exact: true })).toHaveCount(0)
  await expect(profiles.getByRole('form', { name: 'Upload writing profile' }).getByRole('status')).toHaveText('Writing profile uploaded.')
  await expect(profiles.getByRole('heading', { name: uploadedWritingProfile.name })).toBeVisible()
})

test('the operator can save, replace, and remove shared AI configuration', async ({ page }) => {
  const state = await mockAPI(page)
  state.aiOperator = true
  state.providers = []
  state.defaultProvider = null
  await page.goto('/#settings')
  const key = page.getByLabel('OpenAI (GPT) API key', { exact: true })
  await expect(key).toHaveAttribute('type', 'password')
  expect(state.modelRequests).toHaveLength(0)
  await key.fill('test-openai-key')
  await page.getByRole('button', { name: 'Save OpenAI (GPT)', exact: true }).click()
  await expect(page.getByText('OpenAI (GPT) settings saved.')).toBeVisible()
  await expect(key).toHaveValue('')
  expect(state.providerUpdates[0]).toEqual({ provider: 'openai', api_key: 'test-openai-key', model: 'gpt-6-luna' })
  await page.getByLabel('OpenAI (GPT) model', { exact: true }).fill('gpt-6.1-sol')
  await page.getByRole('option', { name: 'gpt-6.1-sol', exact: true }).click()
  await page.getByRole('button', { name: 'Save OpenAI (GPT)', exact: true }).click()
  await expect(page.getByLabel('OpenAI (GPT) model', { exact: true })).toHaveValue('gpt-6.1-sol')
  expect(state.providerUpdates.at(-1)?.api_key).toBe('')
  await key.fill('replacement-test-key')
  await page.getByRole('button', { name: 'Save OpenAI (GPT)', exact: true }).click()
  await expect(key).toHaveValue('')
  expect(state.providerUpdates.at(-1)?.api_key).toBe('replacement-test-key')
  await page.getByLabel('Default AI provider').selectOption('openai')
  await page.getByRole('button', { name: 'Save default provider' }).click()
  await expect(page.getByText('Default AI provider saved.')).toBeVisible()
  await page.reload()
  await expect(key).toHaveValue('')
  await expect(page.getByLabel('OpenAI (GPT) model', { exact: true })).toHaveValue('gpt-6.1-sol')
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: test.info().outputPath('ai-settings-operator-mobile.png'), fullPage: true })
  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name: 'Remove key and disable' }).click()
  await expect(page.getByText('OpenAI (GPT) disabled. Its saved key has been removed.')).toBeVisible()
  await expect(key).toHaveValue('')
  expect(state.providers).toHaveLength(0)
  expect(state.defaultProvider).toBeNull()
})

test('shared key forms are hidden from ordinary accounts', async ({ page }) => {
  const state = await mockAPI(page)
  await page.goto('/#settings')
  await expect(page.getByRole('heading', { name: 'AI providers' })).toBeVisible()
  await expect(page.getByText('gpt-6-luna', { exact: true })).toBeVisible()
  await expect(page.getByLabel('OpenAI (GPT) API key', { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('OpenRouter API key', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Save default provider' })).toHaveCount(0)
  expect(state.modelRequests).toHaveLength(0)
})

test('OpenRouter can be configured on its own with discovered and manual model variants', async ({ page }) => {
  const state = await mockAPI(page)
  state.aiOperator = true
  state.providers = []
  state.defaultProvider = null
  await page.goto('/#settings')
  const form = page.getByRole('form', { name: 'OpenRouter configuration' })
  await form.getByLabel('OpenRouter API key', { exact: true }).fill('openrouter-test-key')
  await form.getByRole('button', { name: 'Save OpenRouter', exact: true }).click()
  await expect(page.getByText('OpenRouter settings saved.')).toBeVisible()
  await expect(form.getByRole('button', { name: 'Refresh OpenRouter models' })).toBeEnabled()
  await form.getByLabel('OpenRouter model', { exact: true }).fill('Free')
  await page.getByRole('option', { name: /Free text model/ }).click()
  await form.getByRole('button', { name: 'Save OpenRouter', exact: true }).click()
  await expect(form.getByLabel('OpenRouter model', { exact: true })).toHaveValue('vendor/model:free')
  expect(state.providerUpdates.at(-1)?.model).toBe('vendor/model:free')
  await form.getByRole('button', { name: 'Enter model ID manually' }).click()
  await form.getByLabel('OpenRouter model', { exact: true }).fill('vendor/custom-model:free')
  await form.getByRole('button', { name: 'Save OpenRouter', exact: true }).click()
  await expect(form.getByLabel('OpenRouter model', { exact: true })).toHaveValue('vendor/custom-model:free')
  expect(state.providerUpdates.at(-1)?.model).toBe('vendor/custom-model:free')
  await page.getByLabel('Default AI provider').selectOption('openrouter')
  await page.getByRole('button', { name: 'Save default provider' }).click()
  await expect(page.getByText('Default AI provider saved.')).toBeVisible()
  await page.goto('/#new')
  await expect(page.getByLabel('AI provider')).toHaveValue('openrouter')
  await expect(page.getByLabel('AI provider').locator('option')).toHaveCount(2)
  await page.getByLabel('Contact', { exact: true }).selectOption(contact.id)
  await page.getByLabel('Résumé text').fill('Fictional resume.')
  await page.getByRole('button', { name: 'Generate draft' }).click()
  await expect(page.getByRole('heading', { name: draft.subject })).toBeVisible()
  expect(state.generationRequests[0].provider).toBe('openrouter')
})

test('model discovery supports search, keyboard selection, and refresh without changing the model', async ({ page }) => {
  const state = await mockAPI(page)
  state.aiOperator = true
  await page.goto('/#settings')
  const form = page.getByRole('form', { name: 'Anthropic (Claude) configuration' })
  const model = form.getByLabel('Anthropic (Claude) model', { exact: true })
  await expect(form.getByRole('button', { name: 'Refresh Anthropic (Claude) models' })).toBeEnabled()
  await model.fill('Opus')
  await expect(page.getByRole('option', { name: /Claude Opus 5.5/ })).toBeVisible()
  await model.press('ArrowDown')
  await model.press('Enter')
  await expect(model).toHaveValue('claude-opus-5-5')
  expect(state.providerUpdates).toHaveLength(0)
  state.providerModels.anthropic = [{ id: 'claude-new-model', name: 'Claude New Model' }]
  await form.getByRole('button', { name: 'Refresh Anthropic (Claude) models' }).click()
  await expect(form.getByText('The current model is not in the fetched list. Your selection is kept until you change it.')).toBeVisible()
  await expect(model).toHaveValue('claude-opus-5-5')
  expect(state.modelRequests.some((request) => request.provider === 'anthropic' && request.method === 'POST')).toBe(true)
  await form.getByRole('button', { name: 'Save Anthropic (Claude)', exact: true }).click()
  await expect(page.getByText('Anthropic (Claude) settings saved.')).toBeVisible()
  expect(state.providerUpdates.at(-1)?.model).toBe('claude-opus-5-5')
  await page.setViewportSize({ width: 390, height: 844 })
  await form.getByRole('button', { name: 'Show Anthropic (Claude) models' }).click()
  await expect(page.getByRole('option', { name: /Claude New Model/ })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: test.info().outputPath('ai-model-picker-mobile.png'), fullPage: true })
})

test('discovery errors allow manual entry and an empty refresh preserves the selected ID', async ({ page }) => {
  const state = await mockAPI(page)
  state.aiOperator = true
  state.providerModelError = true
  await page.goto('/#settings')
  const form = page.getByRole('form', { name: 'OpenAI (GPT) configuration' })
  await expect(form.getByRole('alert')).toHaveText('Could not fetch models. Try again or enter a model ID manually.')
  await form.getByRole('button', { name: 'Enter model ID manually' }).click()
  await form.getByLabel('OpenAI (GPT) model', { exact: true }).fill('gpt-custom-text')
  state.providerModelError = false
  state.providerModels.openai = []
  await form.getByRole('button', { name: 'Refresh OpenAI (GPT) models' }).click()
  await expect(form.getByText('No compatible models were found. You can enter a model ID manually.')).toBeVisible()
  await expect(form.getByLabel('OpenAI (GPT) model', { exact: true })).toHaveValue('gpt-custom-text')
  await form.getByRole('button', { name: 'Save OpenAI (GPT)', exact: true }).click()
  await expect(page.getByText('OpenAI (GPT) settings saved.')).toBeVisible()
  expect(state.providerUpdates.at(-1)).toEqual({ provider: 'openai', model: 'gpt-custom-text', api_key: '' })
})

test('a failed key save preserves inputs and a successful retry clears the key', async ({ page }) => {
  const state = await mockAPI(page)
  state.aiOperator = true
  state.providerSaveError = true
  await page.goto('/#settings')
  const key = page.getByLabel('Anthropic (Claude) API key', { exact: true })
  await key.fill('test-anthropic-key')
  await page.getByRole('button', { name: 'Show Anthropic (Claude) API key' }).click()
  await expect(key).toHaveAttribute('type', 'text')
  await page.getByRole('button', { name: 'Hide Anthropic (Claude) API key' }).click()
  await page.getByRole('button', { name: 'Save Anthropic (Claude)', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('Could not save AI settings.')
  await expect(key).toHaveValue('test-anthropic-key')
  state.providerSaveError = false
  await page.getByRole('button', { name: 'Save Anthropic (Claude)', exact: true }).click()
  await expect(page.getByText('Anthropic (Claude) settings saved.')).toBeVisible()
  await expect(key).toHaveValue('')
  await expect(key).toHaveAttribute('type', 'password')
})

test('editing saves formatting in a new version that requires fresh approval', async ({ page }) => {
  const state = await mockAPI(page)
  await page.goto('/')
  await page.getByRole('button', { name: /An introduction/ }).click()
  await page.getByRole('checkbox', { name: /I reviewed version 1/ }).check()
  await page.getByRole('button', { name: 'Edit draft', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Approve and send' })).toHaveCount(0)
  await page.getByLabel('To', { exact: true }).fill('revised@example.com')
  await page.getByLabel('Subject', { exact: true }).fill('Revised subject')
  const editor = page.getByRole('textbox', { name: 'Email body' })
  await editor.fill('Hi Alex, revised message.')
  await editor.press('ControlOrMeta+a')
  await page.getByRole('button', { name: 'Bold', exact: true }).click()
  await expect(editor.locator('strong')).toHaveText('Hi Alex, revised message.')
  await page.getByRole('button', { name: 'Save new version' }).click()
  await expect(page.getByRole('heading', { name: 'Revised subject', exact: true })).toBeVisible()
  await expect(page.locator('article strong')).toHaveText('Hi Alex, revised message.')
  await expect(page.getByRole('checkbox', { name: /I reviewed version 2/ })).not.toBeChecked()
  await expect(page.getByRole('button', { name: 'Approve and send' })).toBeDisabled()
  expect(state.editRequests[0].content_hash).toBe(draft.content_hash)
  expect(state.editRequests[0].body_html).toContain('<strong>')
  expect(state.editRequests[0].recipient_email).toBe('revised@example.com')
  await expect(page.locator('article').getByText('revised@example.com', { exact: true })).toBeVisible()
  await expect(page.getByRole('checkbox', { name: /I reviewed version 2/ })).toHaveAccessibleName(/revised@example.com/)
  expect(state.drafts.find((item) => item.version === 1 && item.draft_id === draft.draft_id)?.subject).toBe(draft.subject)
  expect(state.drafts.find((item) => item.version === 1 && item.draft_id === draft.draft_id)?.recipient_email).toBe(draft.recipient_email)
  expect(state.contacts[0].email).toBe(contact.email)
  expect(state.approvalRequests).toHaveLength(0)
  await page.getByRole('checkbox', { name: /I reviewed version 2/ }).check()
  await page.getByRole('button', { name: 'Approve and send' }).click()
  expect(state.approvalRequests).toEqual([{ content_hash: 'b'.repeat(64) }])
  await page.getByRole('link', { name: 'Contacts', exact: true }).click()
  await page.getByRole('link', { name: 'Drafts', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Revised subject', exact: true })).toBeVisible()
})

test('writing an email supports lists, links, undo, redo, and mobile layout', async ({ page }) => {
  const state = await mockAPI(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/#new')
  await page.getByRole('button', { name: 'Write yourself' }).click()
  await page.getByLabel('Contact', { exact: true }).selectOption(contact.id)
  await page.getByLabel('Subject', { exact: true }).fill('A handwritten email')
  const editor = page.getByRole('textbox', { name: 'Email body' })
  await editor.fill('My work')
  await editor.press('ControlOrMeta+a')
  await page.getByRole('button', { name: 'Italic', exact: true }).click()
  await expect(editor.locator('em')).toHaveText('My work')
  await page.getByRole('button', { name: 'Undo', exact: true }).click()
  await expect(editor.locator('em')).toHaveCount(0)
  await page.getByRole('button', { name: 'Redo', exact: true }).click()
  await expect(editor.locator('em')).toHaveText('My work')
  await page.getByRole('button', { name: 'Bullet list', exact: true }).click()
  await expect(editor.locator('ul li')).toHaveText('My work')
  await page.getByRole('button', { name: 'Numbered list', exact: true }).click()
  await expect(editor.locator('ol li')).toHaveText('My work')
  await page.getByRole('button', { name: 'Link', exact: true }).click()
  await page.getByLabel('Link URL').fill('javascript:alert(1)')
  await expect(page.getByRole('button', { name: 'Apply link' })).toBeDisabled()
  await page.getByLabel('Link URL').fill('https://example.com/work')
  await page.getByRole('button', { name: 'Apply link' }).click()
  await expect(editor.locator('a')).toHaveAttribute('href', 'https://example.com/work')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: test.info().outputPath('email-editor-mobile.png'), fullPage: true })
  await page.getByRole('button', { name: 'Save draft', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'A handwritten email' })).toBeVisible()
  await expect(page.locator('article a')).toHaveAttribute('href', 'https://example.com/work')
  expect(state.manualRequest?.body_html).toContain('<ol>')
  expect(state.generated).toBe(false)
  expect(state.approvalRequests).toHaveLength(0)
})

test('stale edit errors preserve text and cancelling protects unsaved changes', async ({ page }) => {
  const state = await mockAPI(page)
  state.editError = true
  await page.goto('/')
  await page.getByRole('button', { name: /An introduction/ }).click()
  await page.getByRole('button', { name: 'Edit draft', exact: true }).click()
  await page.getByRole('textbox', { name: 'Email body' }).fill('Keep my unsaved edits')
  await page.getByRole('button', { name: 'Save new version' }).click()
  await expect(page.getByRole('alert')).toContainText('A newer version exists')
  await expect(page.getByRole('textbox', { name: 'Email body' })).toHaveText('Keep my unsaved edits')
  page.once('dialog', (dialog) => dialog.dismiss())
  await page.getByRole('button', { name: 'Cancel editing' }).click()
  await expect(page.getByRole('textbox', { name: 'Email body' })).toBeVisible()
  page.once('dialog', (dialog) => dialog.dismiss())
  await page.getByRole('link', { name: 'Contacts', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Email body' })).toBeVisible()
  page.once('dialog', (dialog) => dialog.dismiss())
  await page.getByRole('button', { name: 'Account: Owner' }).click()
  await page.getByRole('menuitem', { name: 'Sign out', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Email body' })).toBeVisible()
  await page.keyboard.press('Escape')
  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name: 'Cancel editing' }).click()
  await expect(page.getByRole('heading', { name: draft.subject })).toBeVisible()
  expect(state.approvalRequests).toHaveLength(0)
})

test('rich preview removes active content and preserves readable formatting', async ({ page }) => {
  const state = await mockAPI(page)
  state.drafts = [{ ...draft, body_html: '<p>Hello <strong>Alex</strong><img src="invalid" onerror="window.badPreview=true"></p><script>window.badPreview=true</script><a href="javascript:alert(1)">bad link</a>' }]
  await page.goto('/')
  await page.getByRole('button', { name: /An introduction/ }).click()
  await expect(page.locator('article strong')).toHaveText('Alex')
  await expect(page.locator('article img, article script, article a[href]')).toHaveCount(0)
  expect(await page.evaluate(() => 'badPreview' in window)).toBe(false)
})

test('dark mode follows the system and persists explicit choices through sign in', async ({ page }) => {
  const state = await mockAPI(page)
  state.authenticated = false
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.goto('/')
  const appearance = page.getByRole('button', { name: /^Theme:/ })
  await expect(appearance).toHaveAccessibleName('Theme: system. Switch to light mode')
  await expect(page.locator('html')).toHaveClass('dark')
  await expect(page.locator('body')).toHaveCSS('background-color', 'rgb(17, 24, 22)')
  await appearance.click()
  await expect(page.locator('html')).not.toHaveClass('dark')
  await page.reload()
  await expect(appearance).toHaveAccessibleName('Theme: light. Switch to dark mode')
  await expect(page.locator('html')).not.toHaveClass('dark')
  await appearance.click()
  await appearance.click()
  await expect(page.locator('html')).toHaveClass('dark')
  await page.emulateMedia({ colorScheme: 'light' })
  await expect(page.locator('html')).not.toHaveClass('dark')
  await appearance.click()
  await appearance.click()
  await page.getByRole('link', { name: 'Sign in', exact: true }).click()
  await page.getByLabel('Username', { exact: true }).fill('Owner')
  await page.getByLabel('Password', { exact: true }).fill('letter1!')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Drafts', exact: true })).toBeVisible()
  await expect(appearance).toHaveAccessibleName('Theme: dark. Switch to system mode')
  await page.reload()
  await expect(page.locator('html')).toHaveClass('dark')
  await page.getByRole('button', { name: /An introduction/ }).click()
  await page.getByRole('button', { name: 'Edit draft', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Email body' })).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('dark-email-editor.png'), fullPage: true })
})

test('a missing recipient can be supplied while editing and invalid addresses cannot be saved', async ({ page }) => {
  const state = await mockAPI(page)
  await page.goto('/')
  await page.getByRole('button', { name: /Needs a recipient/ }).click()
  await page.getByRole('button', { name: 'Edit draft', exact: true }).click()
  const to = page.getByLabel('To', { exact: true })
  const save = page.getByRole('button', { name: 'Save new version' })
  await expect(to).toHaveValue('')
  await expect(save).toBeDisabled()
  await to.fill('not-an-email')
  await save.click()
  expect(await to.evaluate((input: HTMLInputElement) => input.checkValidity())).toBe(false)
  expect(state.editRequests).toHaveLength(0)
  await to.fill('recipient@example.com')
  await save.click()
  await expect(page.locator('article').getByText('recipient@example.com', { exact: true })).toBeVisible()
  await expect(page.getByRole('checkbox', { name: /I reviewed version 2/ })).not.toBeChecked()
  expect(state.approvalRequests).toHaveLength(0)
})

test('landing introduces the product and connects Google signup to the workspace', async ({ page }) => {
  const state = await mockAPI(page)
  state.signupAvailable = true
  state.authenticated = false
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'A better first hello.' })).toBeVisible()
  await expect(page.getByText('Example draft', { exact: true })).toBeVisible()
  await page.getByRole('link', { name: 'See how it works' }).click()
  await expect(page.getByRole('heading', { name: 'Your voice. A little head start.' })).toBeInViewport()
  await page.getByRole('link', { name: 'Back to top ↑' }).click()
  await expect(page.getByRole('heading', { name: 'A better first hello.' })).toBeInViewport()
  await page.screenshot({ path: test.info().outputPath('landing-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.getByRole('button', { name: /^Theme:/ }).click()
  await page.getByRole('button', { name: /^Theme:/ }).click()
  await expect(page.locator('html')).toHaveClass('dark')
  await page.screenshot({ path: test.info().outputPath('landing-mobile-dark.png'), fullPage: true, animations: 'disabled' })
  await page.getByRole('button', { name: 'Sign up with Google', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Drafts', exact: true })).toBeVisible()
  expect(state.googleRequests).toEqual(['signup'])
  expect(state.approvalRequests).toHaveLength(0)
})

test('account menu supports keyboard navigation and links Google separately from Gmail', async ({ page }) => {
  const state = await mockAPI(page)
  await page.goto('/')
  await expect(page.getByRole('navigation', { name: 'Main navigation' }).getByText('Settings', { exact: true })).toHaveCount(0)
  const account = page.getByRole('button', { name: 'Account: Owner' })
  await account.focus()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('menu')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('menu')).toBeHidden()
  await expect(account).toBeFocused()
  await account.click()
  await page.getByRole('menuitem', { name: 'Settings', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible()
  await expect(page.getByRole('menu')).toBeHidden()
  await page.getByRole('button', { name: 'Link Google account' }).click()
  await expect(page.getByText('Google sign-in is linked to your account.')).toBeVisible()
  await expect(page.getByText('Google sign-in linked as')).toContainText('owner@example.com')
  await expect(page.getByRole('heading', { name: 'Gmail connection' })).toBeVisible()
  expect(state.googleRequests).toEqual(['link'])
  await expect(page.getByRole('heading', { name: 'Your workspace and data' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Source code', exact: true })).toHaveAttribute('href', 'https://github.com/kizilkayaom/sefir')
  await expect(page.getByRole('link', { name: 'Setup guide', exact: true })).toHaveAttribute('href', 'https://github.com/kizilkayaom/sefir#run-with-docker-compose')
  await page.getByRole('link', { name: 'About self-hosting' }).click()
  await expect(page.getByRole('heading', { name: 'Your workspace, on your own machine.' })).toBeInViewport()
  await page.getByRole('link', { name: 'Open your workspace' }).first().click()
  await expect(page.getByRole('heading', { name: 'Drafts', exact: true })).toBeVisible()
})

test('Google sign-in failures and unavailable configuration keep password sign-in usable', async ({ page }) => {
  const state = await mockAPI(page)
  state.authenticated = false
  state.googleError = true
  await page.goto('/#signin')
  await page.getByRole('button', { name: 'Sign in with Google', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('Google sign-in is temporarily unavailable. Please try again.')
  await expect(page.getByLabel('Password', { exact: true })).toBeVisible()
  state.googleAvailable = false
  await page.reload()
  await expect(page.getByRole('button', { name: 'Sign in with Google', exact: true })).toBeDisabled()
  await expect(page.getByLabel('Password', { exact: true })).toBeVisible()
  state.googleAvailable = true
  state.googleError = false
  state.passwordAvailable = false
  await page.goto('/?auth=cancelled#signin')
  await expect(page.getByRole('alert')).toHaveText('Google sign-in was cancelled. You can try again.')
  await expect(page.getByLabel('Password', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Sign in with Google', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Drafts', exact: true })).toBeVisible()
  expect(state.googleRequests).toEqual(['signin', 'signin'])
})

test('sign in offers signup and Google works without connecting an existing account', async ({ page }) => {
  const state = await mockAPI(page)
  state.signupAvailable = true
  state.authenticated = false
  await page.goto('/#signin')
  await page.getByRole('link', { name: 'Sign up', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Create your account' })).toBeVisible()
  await expect(page.getByLabel('Confirm password', { exact: true })).toBeVisible()
  await page.getByRole('link', { name: 'Sign in', exact: true }).click()
  await expect(page.getByLabel('Confirm password', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Sign in with Google', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Drafts', exact: true })).toBeVisible()
  expect(state.googleRequests).toEqual(['signin'])
  expect(state.setupRequests).toHaveLength(0)
})

test('self-hosting links are available while registration stays closed', async ({ page }) => {
  const state = await mockAPI(page)
  state.authenticated = false
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Existing accounts only' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sign up with Google' })).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'View source on GitHub' })).toHaveAttribute('href', 'https://github.com/kizilkayaom/sefir')
  await expect(page.getByRole('link', { name: 'Read the setup guide' })).toHaveAttribute('href', 'https://github.com/kizilkayaom/sefir#run-with-docker-compose')
  await expect(page.getByRole('link', { name: 'Create your account' })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'Know where your data goes.' })).toBeVisible()
  await expect(page.getByText(/Generating a draft sends the résumé text and contact details you provide to your selected AI provider/)).toBeVisible()
  await page.getByRole('link', { name: 'Run Sefir yourself', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Your workspace, on your own machine.' })).toBeInViewport()
  await expect(page.getByText(/You can inspect and adapt the source under the Apache-2.0 license/)).toBeVisible()
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Your workspace, on your own machine.' })).toBeInViewport()
  await page.screenshot({ path: test.info().outputPath('landing-self-hosted-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.screenshot({ path: test.info().outputPath('landing-self-hosted-mobile-dark.png'), fullPage: true, animations: 'disabled' })
  await page.goto('/#signup')
  await expect(page.getByRole('heading', { name: 'Registration is closed' })).toBeVisible()
  await expect(page.getByLabel('Username', { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('Password', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Sign up with Google' })).toHaveCount(0)
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: test.info().outputPath('signup-closed.png'), fullPage: true })
  state.signupAvailable = undefined
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Registration is closed' })).toBeVisible()
  await page.getByRole('link', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('link', { name: 'Sign up', exact: true })).toHaveCount(0)
  await page.getByRole('link', { name: 'Run your own Sefir', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Your workspace, on your own machine.' })).toBeInViewport()
  await page.getByRole('link', { name: 'Sign in to your account' }).click()
  await page.getByLabel('Username', { exact: true }).fill('Owner')
  await page.getByLabel('Password', { exact: true }).fill('letter1!')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Drafts', exact: true })).toBeVisible()
  expect(state.setupRequests).toHaveLength(0)
  expect(state.googleRequests).toHaveLength(0)
})

test('unregistered Google users see closed registration while returning users can sign in', async ({ page }) => {
  const state = await mockAPI(page)
  state.authenticated = false
  state.googleAccountExists = false
  await page.goto('/#signin')
  await page.getByRole('button', { name: 'Sign in with Google', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('Registration is closed on this instance. Please use an existing Sefir account to sign in.')
  await expect(page.getByRole('heading', { name: 'Drafts', exact: true })).toHaveCount(0)
  state.googleAccountExists = true
  await page.getByRole('button', { name: 'Sign in with Google', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Drafts', exact: true })).toBeVisible()
  expect(state.googleRequests).toEqual(['signin', 'signin'])
  expect(state.setupRequests).toHaveLength(0)
})
