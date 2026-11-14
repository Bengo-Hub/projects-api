-- Modify "qa_invites" table: logout revokes the invite's earlier access tokens.
ALTER TABLE "qa_invites" ADD COLUMN "access_not_before" timestamptz NULL;
