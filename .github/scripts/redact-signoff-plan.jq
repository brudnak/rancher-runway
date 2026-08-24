walk(
  if type == "object" then
    del(.terraform_state_key, .state_key_root, .aws_prefix, .run_id)
  else
    .
  end
)
