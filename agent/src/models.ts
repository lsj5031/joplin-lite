import { createModels, createProvider } from '@earendil-works/pi-ai/models';
import { stream, streamSimple } from '@earendil-works/pi-ai/api/openai-completions';

export interface ModelConfig {
  baseUrl: string;
  modelId: string;
  contextWindow: number;
  maxTokens: number;
}

export const PROVIDER_ID = 'joplin';

/**
 * Go owns the credential. The SDK only sees this placeholder and the host's
 * endpoint-scoped transport replaces the Authorization header before the
 * request leaves the process.
 */
export const HOST_MANAGED_KEY = 'joplin-host-managed-key';

export function createAskModels(config: ModelConfig) {
  const models = createModels();
  models.setProvider(
    createProvider({
      id: PROVIDER_ID,
      name: 'Configured Ask model',
      baseUrl: config.baseUrl,
      auth: {
        apiKey: {
          name: 'Ask model API key',
          resolve: async () => ({ auth: { apiKey: HOST_MANAGED_KEY } }),
        },
      },
      models: [
        {
          id: config.modelId,
          name: config.modelId,
          api: 'openai-completions',
          provider: PROVIDER_ID,
          baseUrl: config.baseUrl,
          reasoning: false,
          input: ['text'],
          cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
          contextWindow: config.contextWindow,
          maxTokens: config.maxTokens,
          compat: { supportsDeveloperRole: false, supportsReasoningEffort: false },
        },
      ],
      api: { stream, streamSimple },
    }),
  );
  return models;
}
