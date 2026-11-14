// Call frame keys (docs/SEALED.md sections 5.2 and 10): keys only. The
// frame format is the calls code's (chat/call); it asks the web chat's
// sealed session for these through callFrameKeys in chat/sealed/index.js,
// which holds the current epoch secret.

import { callKeyIndex, callSecret, callSenderKey } from './epoch.js';

// deriveCallKeys gives, for one call in one key epoch, every device's call
// sender key and the key index frames carry: { keyEpoch, keyIndex, keys:
// Map device → 32 bytes }. A device encrypts its own frames under its own
// key and decrypts each other device's under theirs.
export async function deriveCallKeys(epochSecret, conversationId, keyEpoch, callId, deviceIds) {
  const secret = await callSecret(epochSecret, conversationId, keyEpoch, callId);
  const keys = new Map();
  for (const id of deviceIds) keys.set(id, await callSenderKey(secret, conversationId, keyEpoch, id));
  return { keyEpoch, keyIndex: callKeyIndex(keyEpoch), keys };
}
