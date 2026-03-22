package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/scheduler"
)

// ListFolders lists all folders for an account
func (s *MessageService) ListFolders(
	ctx context.Context,
	accountID string,
) ([]*models.FolderInfo, error) {
	// Check fair-use tokens
	success, _, err := s.scheduler.ConsumeTokens(accountID, scheduler.OpList, "low")
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, models.NewThrottleError(60)
	}

	// Try cache first
	if s.cache != nil {
		cachedFolders, err := s.getCachedFolders(ctx, accountID)
		if err == nil && cachedFolders != nil {
			return cachedFolders, nil
		}
	}

	// Get account with decrypted credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Create IMAP connection
	imapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	imapConfig := pool.IMAPConfig{
		Host:       account.IMAPConfig.Host,
		Port:       account.IMAPConfig.Port,
		Username:   account.IMAPConfig.Username,
		Password:   account.IMAPConfig.Password,
		Encryption: account.IMAPConfig.Encryption,
	}

	client, release, err := s.sessions.Acquire(imapCtx, accountID, imapConfig)
	if err != nil {
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return nil, models.NewAuthError("Invalid mail server credentials")
		}
		return nil, models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer release()

	// List folders
	imapFolders, err := client.ListFolders()
	if err != nil {
		return nil, fmt.Errorf("failed to list folders: %w", err)
	}

	// Convert to models.FolderInfo with message counts
	folders := make([]*models.FolderInfo, 0, len(imapFolders))
	for _, f := range imapFolders {
		// Get folder stats by selecting the folder
		folderInfo := &models.FolderInfo{
			Name:        f.Name,
			Delimiter:   f.Delimiter,
			Attributes:  f.Attributes,
			Messages:    f.Messages,
			Recent:      f.Recent,
			Unseen:      f.Unseen,
			UIDNext:     f.UIDNext,
			UIDValidity: f.UIDValidity,
			LastSync:    time.Now(),
		}

		// Select folder to get accurate counts (Unseen, Messages, etc.)
		selectedInfo, err := client.SelectFolder(f.Name)
		if err == nil && selectedInfo != nil {
			// Use the selected folder info for accurate counts
			folderInfo.Messages = selectedInfo.Messages
			folderInfo.Recent = selectedInfo.Recent
			folderInfo.Unseen = selectedInfo.Unseen
			folderInfo.UIDNext = selectedInfo.UIDNext
			folderInfo.UIDValidity = selectedInfo.UIDValidity
		} else {
			log.Printf("Warning: failed to select folder %s: %v", f.Name, err)
		}

		folders = append(folders, folderInfo)
	}

	// Cache folder list
	if s.cache != nil {
		if err := s.setCachedFolders(ctx, accountID, folders); err != nil {
			log.Printf("Warning: failed to cache folders: %v", err)
		}
	}

	log.Printf("Listed %d folders for account %s", len(folders), accountID)
	return folders, nil
}

// BuildFolderTree builds a hierarchical tree structure from flat folder list
func BuildFolderTree(folders []*models.FolderInfo) []*models.FolderTreeNode {
	if len(folders) == 0 {
		return nil
	}

	// Create a map for quick lookup
	folderMap := make(map[string]*models.FolderTreeNode)
	for _, f := range folders {
		folderMap[f.Name] = &models.FolderTreeNode{
			Folder:   f,
			Children: []*models.FolderTreeNode{},
			Path:     f.Name,
			Depth:    0,
		}
	}

	var rootFolders []*models.FolderTreeNode

	// Determine delimiter (use most common one from folders)
	delimiter := "/"
	for _, f := range folders {
		if f.Delimiter != "" && f.Delimiter != " " {
			delimiter = f.Delimiter
			break
		}
	}

	// Build tree structure
	for _, folder := range folders {
		node, exists := folderMap[folder.Name]
		if !exists {
			continue
		}

		// Set initial path to folder name
		node.Path = folder.Name

		// Check if this folder has a parent
		parts := strings.Split(folder.Name, delimiter)
		if len(parts) > 1 {
			// Try to find parent folder
			parentName := strings.Join(parts[:len(parts)-1], delimiter)
			if parent, exists := folderMap[parentName]; exists {
				// Add as child of parent
				node.Depth = parent.Depth + 1
				parent.Children = append(parent.Children, node)
				continue
			}
		}

		// No parent found, this is a root-level folder
		rootFolders = append(rootFolders, node)
	}

	// Sort root folders
	sortFolderTree(rootFolders)

	return rootFolders
}

// sortFolderTree sorts folder tree nodes alphabetically, with standard folders first
func sortFolderTree(nodes []*models.FolderTreeNode) {
	sort.Slice(nodes, func(i, j int) bool {
		iName := strings.ToUpper(nodes[i].Folder.Name)
		jName := strings.ToUpper(nodes[j].Folder.Name)

		// Standard folders order
		standardOrder := []string{"INBOX", "DRAFTS", "SENT", "TRASH", "JUNK", "ARCHIVE"}

		iIsStandard := false
		jIsStandard := false
		iIndex := -1
		jIndex := -1

		for idx, sf := range standardOrder {
			if iName == sf {
				iIsStandard = true
				iIndex = idx
			}
			if jName == sf {
				jIsStandard = true
				jIndex = idx
			}
		}

		// Standard folders come first
		if iIsStandard && !jIsStandard {
			return true
		}
		if !iIsStandard && jIsStandard {
			return false
		}

		// Sort standard folders by predefined order
		if iIsStandard && jIsStandard {
			return iIndex < jIndex
		}

		// Sort non-standard folders alphabetically
		return iName < jName
	})

	// Recursively sort children
	for _, node := range nodes {
		sortFolderTree(node.Children)
	}
}

// GetFolderTree returns folder hierarchy for an account
func (s *MessageService) GetFolderTree(
	ctx context.Context,
	accountID string,
) ([]*models.FolderTreeNode, error) {
	folders, err := s.ListFolders(ctx, accountID)
	if err != nil {
		return nil, err
	}

	return BuildFolderTree(folders), nil
}

// GetFolderInfo gets information about a specific folder
func (s *MessageService) GetFolderInfo(
	ctx context.Context,
	accountID string,
	folder string,
) (*models.FolderInfo, error) {
	if folder == "" {
		folder = "INBOX"
	}

	// Check fair-use tokens
	success, _, err := s.scheduler.ConsumeTokens(accountID, scheduler.OpList, "low")
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, models.NewThrottleError(60)
	}

	// Get account with decrypted credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Create IMAP connection
	imapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	imapConfig := pool.IMAPConfig{
		Host:       account.IMAPConfig.Host,
		Port:       account.IMAPConfig.Port,
		Username:   account.IMAPConfig.Username,
		Password:   account.IMAPConfig.Password,
		Encryption: account.IMAPConfig.Encryption,
	}

	client, release, err := s.sessions.Acquire(imapCtx, accountID, imapConfig)
	if err != nil {
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return nil, models.NewAuthError("Invalid mail server credentials")
		}
		return nil, models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer release()

	// Select folder and get info
	info, err := client.SelectFolder(folder)
	if err != nil {
		return nil, fmt.Errorf("failed to select folder: %w", err)
	}

	return &models.FolderInfo{
		Name:        folder,
		Messages:    info.Messages,
		Recent:      info.Recent,
		Unseen:      info.Unseen,
		UIDNext:     info.UIDNext,
		UIDValidity: info.UIDValidity,
		LastSync:    time.Now(),
	}, nil
}

// invalidateMessageListCache invalidates all message list cache entries for a folder
func (s *MessageService) invalidateMessageListCache(
	ctx context.Context,
	accountID string,
	folder string,
) error {
	if s.cache == nil {
		return nil
	}

	// Delete all message list cache entries for this folder
	pattern := fmt.Sprintf("msglist:%s:%s:*", accountID, folder)
	keys, err := s.cache.Keys(ctx, pattern)
	if err != nil {
		return err
	}

	for _, key := range keys {
		if err := s.cache.Delete(ctx, key); err != nil {
			log.Printf("Warning: failed to delete cache key %s: %v", key, err)
		}
	}

	log.Printf("Invalidated %d cache entries for folder %s", len(keys), folder)
	return nil
}

// getCachedFolders tries to get folder list from cache
func (s *MessageService) getCachedFolders(
	ctx context.Context,
	accountID string,
) ([]*models.FolderInfo, error) {
	cacheKey := fmt.Sprintf("folders:%s", accountID)

	data, err := s.cache.Get(ctx, cacheKey)
	if err != nil {
		return nil, err
	}

	var folders []*models.FolderInfo
	if err := json.Unmarshal(data, &folders); err != nil {
		return nil, err
	}

	return folders, nil
}

// setCachedFolders stores folder list in cache
func (s *MessageService) setCachedFolders(
	ctx context.Context,
	accountID string,
	folders []*models.FolderInfo,
) error {
	cacheKey := fmt.Sprintf("folders:%s", accountID)

	data, err := json.Marshal(folders)
	if err != nil {
		return err
	}

	return s.cache.Set(ctx, cacheKey, data, 30*time.Minute)
}
