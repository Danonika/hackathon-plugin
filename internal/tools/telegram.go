package tools

import (
	"context"
	"fmt"
	"strings"

	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/telegram"
)

func registerTelegram(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"telegram.send",
		"Send a Telegram message from the project being built (alerts, digests, demo notifications). The bot token comes from TELEGRAM_BOT_TOKEN; the chat must be the configured chat or listed in TELEGRAM_ALLOWED_CHAT_IDS. Long text is chunked at 4096 characters.",
		map[string]any{
			"text":       strProp("Message text (plain by default)"),
			"chat_id":    strProp("Optional chat/topic id override; must be allowlisted"),
			"parse_mode": strProp("Optional Telegram parse mode, for example HTML"),
			"silent":     boolProp("Send without a notification sound"),
			"reply_to":   numProp("Optional message id to reply to"),
		},
		[]string{"text"},
		func(ctx context.Context, args map[string]any) (any, error) {
			if d.Telegram == nil {
				return mcp.Fail(trace(ctx), "telegram client unavailable"), nil
			}
			parseMode := argString(args, "parse_mode")
			if parseMode != "" && !strings.EqualFold(parseMode, "HTML") && !strings.EqualFold(parseMode, "MarkdownV2") {
				return mcp.Fail(trace(ctx), "parse_mode must be HTML or MarkdownV2"), nil
			}
			result, err := d.Telegram.SendMessage(ctx, argString(args, "text"), telegram.SendOptions{
				ChatID:    argString(args, "chat_id"),
				ParseMode: parseMode,
				Silent:    argBool(args, "silent", false),
				ReplyTo:   argInt(args, "reply_to", 0),
			})
			if err != nil {
				if !d.Telegram.Configured() {
					return mcp.Partial(trace(ctx), map[string]any{
						"configured": false,
						"hint":       "set TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID in .env",
					}, err.Error()), nil
				}
				recordTelegramError(d)
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			if d.Metrics != nil {
				d.Metrics.Inc("telegram_sends_total")
			}
			return mcp.OK(trace(ctx), map[string]any{
				"message_id": result.MessageID,
				"chat_id":    result.ChatID,
				"chunks":     result.Chunks,
			}).WithMetrics(map[string]any{
				"chunks": result.Chunks,
			}), nil
		},
	))

	reg.Register(tool(
		"telegram.validate",
		"Verify the Telegram bot token and discover chat ids: getMe, getChat and recent getUpdates. Send a message to the bot first so its chat appears in the updates.",
		map[string]any{
			"chat_id": strProp("Optional chat id to inspect; defaults to TELEGRAM_CHAT_ID"),
		},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			if d.Telegram == nil {
				return mcp.Fail(trace(ctx), "telegram client unavailable"), nil
			}
			if !d.Telegram.Configured() {
				return mcp.Partial(trace(ctx), map[string]any{
					"configured": false,
					"hint":       "set TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID in .env",
				}, "telegram bot token is not configured"), nil
			}
			bot, err := d.Telegram.GetMe(ctx)
			if err != nil {
				recordTelegramError(d)
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			data := map[string]any{
				"configured":      true,
				"bot":             bot,
				"default_chat_id": d.Telegram.DefaultChatID(),
			}
			chatID := argString(args, "chat_id")
			if chatID == "" {
				chatID = d.Telegram.DefaultChatID()
			}
			if chatID != "" {
				if chat, err := d.Telegram.GetChat(ctx, chatID); err == nil {
					data["chat"] = chat
				} else {
					data["chat_error"] = err.Error()
				}
			}
			if updates, err := d.Telegram.GetUpdates(ctx, 0, 5, 0); err == nil {
				data["recent_chats"] = summarizeChats(updates)
			} else {
				data["recent_chats_error"] = err.Error()
			}
			return mcp.OK(trace(ctx), data), nil
		},
	))

	reg.Register(tool(
		"telegram.manage",
		"Manage the bot during setup: read updates, set the command list, or set/delete the webhook. No arbitrary API passthrough.",
		map[string]any{
			"action":       strProp("get_updates | set_commands | set_webhook | delete_webhook"),
			"offset":       numProp("get_updates: first update id to return"),
			"limit":        numProp("get_updates: maximum updates (default 10, max 100)"),
			"timeout_sec":  numProp("get_updates: long-poll timeout in seconds"),
			"commands":     arrProp("set_commands: [{command, description}]", objProp("Bot command")),
			"url":          strProp("set_webhook: https webhook url"),
			"secret_token": strProp("set_webhook: optional secret token"),
			"drop_pending": boolProp("delete_webhook: drop pending updates"),
		},
		[]string{"action"},
		func(ctx context.Context, args map[string]any) (any, error) {
			if d.Telegram == nil {
				return mcp.Fail(trace(ctx), "telegram client unavailable"), nil
			}
			if !d.Telegram.Configured() {
				return mcp.Partial(trace(ctx), map[string]any{"configured": false}, "telegram bot token is not configured"), nil
			}
			action := argString(args, "action")
			switch action {
			case "get_updates":
				updates, err := d.Telegram.GetUpdates(ctx, int64(argInt(args, "offset", 0)), argInt(args, "limit", 10), argInt(args, "timeout_sec", 0))
				if err != nil {
					recordTelegramError(d)
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
				return mcp.OK(trace(ctx), map[string]any{"updates": updates, "count": len(updates)}), nil
			case "set_commands":
				commands, err := parseBotCommands(args)
				if err != nil {
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
				if err := d.Telegram.SetMyCommands(ctx, commands); err != nil {
					recordTelegramError(d)
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
				return mcp.OK(trace(ctx), map[string]any{"commands": commands, "count": len(commands)}), nil
			case "set_webhook":
				if err := d.Telegram.SetWebhook(ctx, argString(args, "url"), argString(args, "secret_token")); err != nil {
					recordTelegramError(d)
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
				return mcp.OK(trace(ctx), map[string]any{"webhook": argString(args, "url")}), nil
			case "delete_webhook":
				if err := d.Telegram.DeleteWebhook(ctx, argBool(args, "drop_pending", false)); err != nil {
					recordTelegramError(d)
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
				return mcp.OK(trace(ctx), map[string]any{"deleted": true}), nil
			default:
				return mcp.Fail(trace(ctx), fmt.Sprintf("unknown action %q: expected get_updates, set_commands, set_webhook or delete_webhook", action)), nil
			}
		},
	))
}

func parseBotCommands(args map[string]any) ([]telegram.BotCommand, error) {
	raw, ok := args["commands"].([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("commands is required for set_commands")
	}
	commands := []telegram.BotCommand{}
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("every command entry must be an object")
		}
		command, _ := m["command"].(string)
		description, _ := m["description"].(string)
		if command == "" || description == "" {
			return nil, fmt.Errorf("every command needs a command and a description")
		}
		commands = append(commands, telegram.BotCommand{Command: command, Description: description})
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("no valid commands provided")
	}
	return commands, nil
}

func summarizeChats(updates []telegram.UpdateInfo) []map[string]any {
	seen := map[string]bool{}
	out := []map[string]any{}
	for _, update := range updates {
		if update.ChatID == "" || seen[update.ChatID] {
			continue
		}
		seen[update.ChatID] = true
		out = append(out, map[string]any{
			"chat_id": update.ChatID,
			"type":    update.ChatType,
			"name":    update.ChatName,
		})
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func recordTelegramError(d Deps) {
	if d.Metrics != nil {
		d.Metrics.Inc("telegram_errors_total")
	}
}
