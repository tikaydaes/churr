// Where the parts of the web chat reach each other: moving between
// screens, and saying things to a screen reader. app.js fills these in at
// start-up; every other module only imports this small file, so none of
// them depends on the others.

export const ui = {
  go() {},                 // go(path): move to an address under /chat/
  openConversation() {},   // openConversation(id, { messageId, threadId })
  openThread() {},         // openThread(conversationId, parentId, { quote })
  closeThread() {},
  refresh() {},            // draw the current screen again
  announce() {},           // announce(text): read out politely
};

// A link to a conversation, a message or a thread, as an address a person
// can copy and open.
export function linkTo(conversationId, messageId, parentId) {
  let path = '/chat/c/' + conversationId;
  if (parentId) path += '/t/' + parentId;
  else if (messageId) path += '/m/' + messageId;
  return location.origin + path;
}
