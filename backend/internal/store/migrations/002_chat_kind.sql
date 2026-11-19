-- A chat message is either typed by the learner ("message") or stands for a
-- button press such as "Write 15 at my level" ("request"), shown as a system card.
ALTER TABLE chat_messages ADD COLUMN kind TEXT NOT NULL DEFAULT 'message' CHECK (kind IN ('message', 'request'));

-- Earlier button presses stored Claude's fixed instruction as the message.
UPDATE chat_messages SET kind = 'request'
WHERE role = 'user' AND (text LIKE 'Write my first questions for %' OR text LIKE 'Write 15 questions for % around my skill,%');
