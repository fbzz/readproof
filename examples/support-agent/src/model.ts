// The model half of the agent: turn the bytes Readproof delivered into an answer.
//
// The only thing that matters for the demo's claim is that the prompt is
// built from `entries` and nothing else — no second read of the policy
// files, no retrieval of its own. Whatever the manifest recorded is exactly
// what the model saw, which is what makes the replay meaningful.

import { Ollama } from "ollama";

import { FAKE_MODEL, OLLAMA_HOST, OLLAMA_MODEL } from "./config.js";

/** One policy document, as Readproof delivered it into this turn. */
export interface ContextEntry {
  uri: string;
  /** The "@<tag>" it was mounted by; absent for a plain URI. */
  ref?: string;
  snapshot_id: string;
  content_hash: string;
  content: string;
}

export interface AnswerResult {
  text: string;
  /** The model that actually answered — resolved, not requested. */
  model: string;
}

export interface AnswerOptions {
  /** Force the deterministic fake model (defaults to $SUPPORT_FAKE_MODEL). */
  fake?: boolean;
  /** Override the model name (defaults to $OLLAMA_MODEL, then discovery). */
  model?: string;
  /** Where streamed tokens go. Defaults to stdout; tests pass a sink. */
  write?: (chunk: string) => void;
}

const FAKE_MODEL_NAME = "fake-deterministic";

const SYSTEM_INSTRUCTION = [
  "Answer only from the policies given.",
  "Name the policy you relied on.",
  "If the policies do not cover it, say so.",
].join(" ");

/**
 * Answer `question` from `entries` and nothing else.
 *
 * Streams tokens to stdout as they arrive so a slow local model still looks
 * alive, and returns the complete text plus the model that produced it.
 */
export async function answer(
  question: string,
  entries: ContextEntry[],
  opts: AnswerOptions = {},
): Promise<AnswerResult> {
  const write = opts.write ?? ((chunk: string) => process.stdout.write(chunk));

  if (opts.fake ?? FAKE_MODEL) {
    const text = fakeAnswer(question, entries);
    console.log(`model: ${FAKE_MODEL_NAME}`);
    write(`${text}\n`);
    return { text, model: FAKE_MODEL_NAME };
  }

  const client = new Ollama({ host: OLLAMA_HOST });
  const model = await resolveModel(client, opts.model);
  console.log(`model: ${model}`);

  const messages = [
    { role: "system", content: systemPrompt(entries) },
    { role: "user", content: userPrompt(question, entries) },
  ];

  let text = "";
  try {
    const stream = await client.chat({ model, messages, stream: true });
    for await (const part of stream) {
      const chunk = part.message.content;
      if (chunk) {
        text += chunk;
        write(chunk);
      }
    }
  } catch (err: unknown) {
    throw ollamaError(err);
  }
  write("\n");

  return { text: text.trim(), model };
}

/**
 * The house style document IS the system prompt — that is the whole point
 * of governing it: change tone.md, promote it, and every later answer
 * changes with it, provably.
 */
function systemPrompt(entries: ContextEntry[]): string {
  const tone = entries.find((e) => e.uri.endsWith("/tone"));
  const style = tone ? tone.content.trim() : "Be concise and plain.";
  return `${style}\n\n${SYSTEM_INSTRUCTION}`;
}

/**
 * Each document is labelled with its readproof:// identity and the first 12 hex
 * of its content hash. That header is what ties a sentence in the answer
 * back to a line in the manifest — and it costs a dozen tokens.
 */
function userPrompt(question: string, entries: ContextEntry[]): string {
  const docs = entries
    .map((e) => `### ${e.uri} (${shortHash(e.content_hash)})\n${e.content.trim()}`)
    .join("\n\n");
  return `${docs}\n\n${question}`;
}

/** "sha256:c8b0bb212e93151d…" -> "sha256:c8b0bb212e93". */
export function shortHash(contentHash: string): string {
  const [algorithm, hex] = contentHash.split(":", 2);
  if (hex === undefined) {
    return contentHash.slice(0, 12);
  }
  return `${algorithm}:${hex.slice(0, 12)}`;
}

/**
 * $OLLAMA_MODEL wins. Otherwise ask Ollama what it has and take the first
 * model that is not an embedding model — `ollama list` happily returns
 * nomic-embed-text, which cannot chat.
 */
async function resolveModel(client: Ollama, override?: string): Promise<string> {
  const explicit = override ?? OLLAMA_MODEL;
  if (explicit) {
    return explicit;
  }

  let available: { name: string }[];
  try {
    available = (await client.list()).models;
  } catch (err: unknown) {
    throw ollamaError(err);
  }

  const chat = available.find((m) => !m.name.toLowerCase().includes("embed"));
  if (!chat) {
    throw new Error(
      "no chat model available on Ollama — set OLLAMA_MODEL or: ollama pull llama3.2",
    );
  }
  return chat.name;
}

/** A connection failure is almost always one of two things — say which. */
function ollamaError(err: unknown): Error {
  const message = err instanceof Error ? err.message : String(err);
  if (/fetch failed|ECONNREFUSED|ENOTFOUND|EAI_AGAIN|socket hang up/i.test(message)) {
    return new Error(
      `cannot reach Ollama at ${OLLAMA_HOST}: ${message} — is \`ollama serve\` running? OLLAMA_HOST=<url> points elsewhere`,
    );
  }
  return err instanceof Error ? err : new Error(message);
}

/**
 * The fake model: no Ollama, no network, byte-identical output for
 * byte-identical input. It answers straight out of the refund policy's
 * first sentence, so when the source document changes the answer visibly
 * changes with it — which is the behavior the demo is about.
 */
function fakeAnswer(question: string, entries: ContextEntry[]): string {
  const refunds = entries.find((e) => e.uri.endsWith("/refunds"));
  if (!refunds) {
    return "No refund policy is mounted, so I cannot answer that.";
  }
  return `${firstSentence(refunds.content)} (per ${refunds.uri}, ${shortHash(refunds.content_hash)}) — asked: ${question}`;
}

/** First sentence of the first prose paragraph, markdown headings skipped. */
function firstSentence(markdown: string): string {
  const prose = markdown
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "" && !line.startsWith("#"))
    .join(" ");
  const end = prose.indexOf(". ");
  return end === -1 ? prose : prose.slice(0, end + 1);
}
