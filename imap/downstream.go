package imap

import (
	"fmt"
	"os"

	"path/filepath"

	"github.com/go-kit/kit/log/level"
	"github.com/go-pluto/maildir"
	"github.com/go-pluto/pluto/comm"
	"github.com/go-pluto/pluto/crdt"
)

// ApplyCreate performs the downstream part
// of a CREATE operation.
func (node *IMAPNode) ApplyCreate(msg comm.Msg) {

	createUpd := msg.Create

	// Build up paths before entering critical section.
	posMaildir := filepath.Join(node.MaildirRoot, createUpd.User, createUpd.Mailbox)
	posMailboxCRDTPath := filepath.Join(node.CRDTLayerRoot, createUpd.User, fmt.Sprintf("%s.log", createUpd.Mailbox))

	// We need to track existence state of various
	// file system objects in case we need to revert.
	maildirExisted := true
	crdtFileExisted := true
	structureExisted := true
	contentsExisted := true

	// Lock node exclusively.
	node.Lock.Lock()
	defer node.Lock.Unlock()

	// Save user's mailbox structure CRDT to more
	// conveniently use it hereafter.
	userMainCRDT := node.MailboxStructure[createUpd.User]["Structure"]

	// Only attempt to create the corresponding
	// Maildir if it does not already exist.
	_, err := os.Stat(posMaildir)
	if os.IsNotExist(err) {

		maildirExisted = false

		// Create a new Maildir on stable storage.
		err = maildir.Dir(posMaildir).Create()
		if err != nil {
			level.Error(node.Logger).Log(
				"msg", "maildir for new mailbox could not be created",
				"err", err,
			)
			os.Exit(1)
		}
	}

	var posMailboxCRDT *crdt.ORSet

	// Only attempt to initialize a new OR-Set
	// if the corresponding file does not already
	// exist in file system.
	_, err = os.Stat(posMailboxCRDTPath)
	if os.IsNotExist(err) {

		crdtFileExisted = false

		// Initialize new ORSet for new mailbox.
		posMailboxCRDT, err = crdt.InitORSetWithFile(posMailboxCRDTPath)
		if err != nil {

			// Perform clean up.
			level.Error(node.Logger).Log(
				"msg", "fail during downstream CREATE execution, will clean up",
				"err", err,
			)

			if !maildirExisted {

				// Only remove created Maildir if it did
				// not exist prior to this function's entrance.
				err = maildir.Dir(posMaildir).Remove()
				if err != nil {
					level.Error(node.Logger).Log(
						"msg", "failed to remove created Maildir",
						"err", err,
					)
				}
			}

			os.Exit(1)
		}
	}

	// If the CRDT is not yet present in mailbox
	// structure, place newly created one there.
	if _, found := node.MailboxStructure[createUpd.User][createUpd.Mailbox]; !found {
		structureExisted = false
		node.MailboxStructure[createUpd.User][createUpd.Mailbox] = posMailboxCRDT
	}

	// If no slice was found in contents structure,
	// initialize one for new mailbox to track message
	// sequence numbers in it.
	if _, found := node.MailboxContents[createUpd.User][createUpd.Mailbox]; !found {
		contentsExisted = false
		node.MailboxContents[createUpd.User][createUpd.Mailbox] = make([]string, 0, 6)
	}

	// If succeeded, add a new folder in user's main CRDT.
	err = userMainCRDT.AddEffect(createUpd.AddMailbox.Value, createUpd.AddMailbox.Tag, true)
	if err != nil {

		level.Error(node.Logger).Log(
			"msg", "fail during downstream CREATE execution, will clean up",
			"err", err,
		)

		// If it did not exist, remove the just
		// added CRDT from structure map.
		if !structureExisted {
			delete(node.MailboxStructure[createUpd.User], createUpd.Mailbox)
		}

		// If it did not exist, remove the just
		// added slice from contents map.
		if !contentsExisted {
			delete(node.MailboxContents[createUpd.User], createUpd.Mailbox)
		}

		// If it did not exist, attempt to remove
		// the created Maildir.
		if !maildirExisted {

			err = maildir.Dir(posMaildir).Remove()
			if err != nil {
				level.Error(node.Logger).Log(
					"msg", "failed to remove created Maildir",
					"err", err,
				)
			}
		}

		// If it did not exist, attempt to remove
		// the created CRDT file.
		if !crdtFileExisted {

			err = os.Remove(posMailboxCRDTPath)
			if err != nil {
				level.Error(node.Logger).Log(
					"msg", "failed to remove CRDT file of mailbox",
					"err", err,
				)
			}
		}

		os.Exit(1)
	}
}

// ApplyDelete performs the downstream part
// of a DELETE operation.
func (node *IMAPNode) ApplyDelete(msg comm.Msg) {

	deleteUpd := msg.Delete

	// Build up paths before entering critical section.
	delMailboxCRDTPath := filepath.Join(node.CRDTLayerRoot, deleteUpd.User, fmt.Sprintf("%s.log", deleteUpd.Mailbox))
	delMaildir := filepath.Join(node.MaildirRoot, deleteUpd.User, deleteUpd.Mailbox)

	// Construct remove set from received values.
	rmElements := make(map[string]string)
	for _, element := range deleteUpd.RmvMailbox {
		rmElements[element.Tag] = element.Value
	}

	// Lock node exclusively.
	node.Lock.Lock()
	defer node.Lock.Unlock()

	// Save user's mailbox structure CRDT to more
	// conveniently use it hereafter.
	userMainCRDT := node.MailboxStructure[deleteUpd.User]["Structure"]

	// Remove received pairs from user's main CRDT.
	err := userMainCRDT.RemoveEffect(rmElements, true)
	if err != nil {
		level.Error(node.Logger).Log(
			"msg", "failed to remove elements from user's main CRDT",
			"err", err,
		)
		os.Exit(1)
	}

	if userMainCRDT.Lookup(deleteUpd.Mailbox) {

		// Concurrent CREATE operations have put more instances
		// of this mailbox into the user's main structure CRDT.
		// Do not remove the underlying files. Instead, delete
		// the mail files sent by the source node as representing
		// the folder's content at the time of DELETE issuance.

		// TODO: CONTINUE HERE.
		// TODO: AMEND MESSAGE SENT BY SOURCE DELETE INVOCATION.

		// Remove slice from contents map if present.
		// if _, found := node.MailboxContents[deleteUpd.User][deleteUpd.Mailbox]; found {
		// 	delete(node.MailboxContents[deleteUpd.User], deleteUpd.Mailbox)
		// }

	} else {

		// This DELETE operation removed the entire presence of
		// this folder from the user's mailbox. Thus, file system
		// clean up of files and folders, and internal state
		// representation manipulation is due.

		// Remove CRDT from structure map if present.
		if _, found := node.MailboxStructure[deleteUpd.User][deleteUpd.Mailbox]; found {
			delete(node.MailboxStructure[deleteUpd.User], deleteUpd.Mailbox)
		}

		// Remove slice from contents map if present.
		if _, found := node.MailboxContents[deleteUpd.User][deleteUpd.Mailbox]; found {
			delete(node.MailboxContents[deleteUpd.User], deleteUpd.Mailbox)
		}

		// If it exists in file system,
		// remove CRDT file of mailbox.
		_, err = os.Stat(delMailboxCRDTPath)
		if err == nil {

			err = os.Remove(delMailboxCRDTPath)
			if err != nil {
				level.Error(node.Logger).Log(
					"msg", "failed to remove CRDT file of mailbox",
					"err", err,
				)
				os.Exit(1)
			}
		}

		// Remove files associated with deleted mailbox
		// from stable storage, if present.
		_, err = os.Stat(delMaildir)
		if err == nil {

			err = maildir.Dir(delMaildir).Remove()
			if err != nil {
				level.Error(node.Logger).Log(
					"msg", "failed to remove Maildir",
					"err", err,
				)
				os.Exit(1)
			}
		}
	}
}

// ApplyAppend performs the downstream part
// of an APPEND operation.
func (node *IMAPNode) ApplyAppend(msg comm.Msg) {

	appendUpd := msg.Append

	// Construct path to potential new file.
	var appendFileName string
	if appendUpd.Mailbox == "INBOX" {
		appendFileName = filepath.Join(node.MaildirRoot, appendUpd.User, "cur", appendUpd.AddMail.Value)
	} else {
		appendFileName = filepath.Join(node.MaildirRoot, appendUpd.User, appendUpd.Mailbox, "cur", appendUpd.AddMail.Value)
	}

	// Lock node exclusively.
	node.Lock.Lock()
	defer node.Lock.Unlock()

	// Save user's mailbox structure CRDT to more
	// conveniently use it hereafter.
	userMainCRDT := node.MailboxStructure[appendUpd.User]["Structure"]

	// Check if specified mailbox from append message is present
	// in user's main CRDT on this node.
	if userMainCRDT.Lookup(appendUpd.Mailbox) {

		// Store concerned mailbox CRDT.
		userMailboxCRDT := node.MailboxStructure[appendUpd.User][appendUpd.Mailbox]

		// Check if mail is not yet present on this node.
		if userMailboxCRDT.Lookup(appendUpd.AddMail.Value) != true {

			// If so, place file content at correct location.
			appendFile, err := os.Create(appendFileName)
			if err != nil {
				level.Error(node.Logger).Log(
					"msg", "failed to create file for mail to append",
					"err", err,
				)
				os.Exit(1)
			}

			_, err = appendFile.Write(appendUpd.AddMail.Contents)
			if err != nil {

				level.Error(node.Logger).Log(
					"msg", "fail during downstream APPEND execution, will clean up",
					"err", err,
				)

				// Remove just created mail file.
				err = os.Remove(appendFileName)
				if err != nil {
					level.Error(node.Logger).Log(
						"msg", "failed to remove created mail file",
						"err", err,
					)
				}

				os.Exit(1)
			}

			// Sync content to stable storage.
			err = appendFile.Sync()
			if err != nil {

				level.Error(node.Logger).Log(
					"msg", "fail during downstream APPEND execution, will clean up",
					"err", err,
				)

				// Remove just created mail file.
				err = os.Remove(appendFileName)
				if err != nil {
					level.Error(node.Logger).Log(
						"msg", "failed to remove created mail file",
						"err", err,
					)
				}

				os.Exit(1)
			}

			// Append new mail to mailbox' contents CRDT.
			node.MailboxContents[appendUpd.User][appendUpd.Mailbox] = append(node.MailboxContents[appendUpd.User][appendUpd.Mailbox], appendUpd.AddMail.Value)

			// If succeeded, add new mail to mailbox' CRDT.
			err = userMailboxCRDT.AddEffect(appendUpd.AddMail.Value, appendUpd.AddMail.Tag, true)
			if err != nil {

				level.Error(node.Logger).Log(
					"msg", "fail during downstream APPEND execution, will clean up",
					"err", err,
				)

				// Remove just created mail file.
				err = os.Remove(appendFileName)
				if err != nil {
					level.Error(node.Logger).Log(
						"msg", "failed to remove created mail file",
						"err", err,
					)
				}

				os.Exit(1)
			}
		} else {

			// Add new mail to mailbox' CRDT.
			err := userMailboxCRDT.AddEffect(appendUpd.AddMail.Value, appendUpd.AddMail.Tag, true)
			if err != nil {
				level.Error(node.Logger).Log(
					"msg", "fail during downstream APPEND execution",
					"err", err,
				)
				os.Exit(1)
			}
		}
	}
}

// ApplyExpunge performs the downstream part
// of an EXPUNGE operation.
func (node *IMAPNode) ApplyExpunge(msg comm.Msg) {

	expungeUpd := msg.Expunge

	// Construct remove set from received values.
	rmElements := make(map[string]string)
	for _, element := range expungeUpd.RmvMail {
		rmElements[element.Tag] = element.Value
	}

	// Construct path to old file.
	var delFileName string
	if expungeUpd.Mailbox == "INBOX" {
		delFileName = filepath.Join(node.MaildirRoot, expungeUpd.User, "cur", expungeUpd.RmvMail[0].Value)
	} else {
		delFileName = filepath.Join(node.MaildirRoot, expungeUpd.User, expungeUpd.Mailbox, "cur", expungeUpd.RmvMail[0].Value)
	}

	// Lock node exclusively.
	node.Lock.Lock()
	defer node.Lock.Unlock()

	// Save user's mailbox structure CRDT to more
	// conveniently use it hereafter.
	userMainCRDT := node.MailboxStructure[expungeUpd.User]["Structure"]

	// Check if specified mailbox from expunge message is
	// present in user's main CRDT on this node.
	if userMainCRDT.Lookup(expungeUpd.Mailbox) {

		// Store concerned mailbox CRDT.
		userMailboxCRDT := node.MailboxStructure[expungeUpd.User][expungeUpd.Mailbox]

		// Delete supplied elements from mailbox.
		err := userMailboxCRDT.RemoveEffect(rmElements, true)
		if err != nil {
			level.Error(node.Logger).Log(
				"msg", "failed to remove mail elements from respective mailbox CRDT",
				"err", err,
			)
			os.Exit(1)
		}

		// Check if just removed elements marked all
		// instances of mail file.
		if userMailboxCRDT.Lookup(expungeUpd.RmvMail[0].Value) != true {

			// If that is the case, remove the file.
			err := os.Remove(delFileName)
			if err != nil {
				level.Error(node.Logger).Log(
					"msg", "failed to remove underlying mail file during downstream EXPUNGE execution",
					"err", err,
				)
				os.Exit(1)
			}
		}

		for msgNum, msgName := range node.MailboxContents[expungeUpd.User][expungeUpd.Mailbox] {

			// Find removed mail file's sequence number.
			if msgName == expungeUpd.RmvMail[0].Value {

				// Delete mail's sequence number from contents structure.
				realMsgNum := msgNum + 1
				node.MailboxContents[expungeUpd.User][expungeUpd.Mailbox] = append(node.MailboxContents[expungeUpd.User][expungeUpd.Mailbox][:msgNum], node.MailboxContents[expungeUpd.User][expungeUpd.Mailbox][realMsgNum:]...)
			}
		}
	}
}

// ApplyStore performs the downstream part
// of a STORE operation.
func (node *IMAPNode) ApplyStore(msg comm.Msg) {

	storeUpd := msg.Store

	// Construct remove set from received values.
	rmElements := make(map[string]string)
	for _, element := range storeUpd.RmvMail {
		rmElements[element.Tag] = element.Value
	}

	// Construct path to old file.
	var delFileName string
	if storeUpd.Mailbox == "INBOX" {
		delFileName = filepath.Join(node.MaildirRoot, storeUpd.User, "cur", storeUpd.RmvMail[0].Value)
	} else {
		delFileName = filepath.Join(node.MaildirRoot, storeUpd.User, storeUpd.Mailbox, "cur", storeUpd.RmvMail[0].Value)
	}

	// Construct path to potential new file.
	var storeFileName string
	if storeUpd.Mailbox == "INBOX" {
		storeFileName = filepath.Join(node.MaildirRoot, storeUpd.User, "cur", storeUpd.AddMail.Value)
	} else {
		storeFileName = filepath.Join(node.MaildirRoot, storeUpd.User, storeUpd.Mailbox, "cur", storeUpd.AddMail.Value)
	}

	// Lock node exclusively.
	node.Lock.Lock()
	defer node.Lock.Unlock()

	// Save user's mailbox structure CRDT to more
	// conveniently use it hereafter.
	userMainCRDT := node.MailboxStructure[storeUpd.User]["Structure"]

	// Check if specified mailbox from store message is present
	// in user's main CRDT on this node.
	if userMainCRDT.Lookup(storeUpd.Mailbox) {

		// Store concerned mailbox CRDT.
		userMailboxCRDT := node.MailboxStructure[storeUpd.User][storeUpd.Mailbox]

		// Delete supplied elements from mailbox.
		err := userMailboxCRDT.RemoveEffect(rmElements, true)
		if err != nil {
			level.Error(node.Logger).Log(
				"msg", "failed to remove mail elements from respective mailbox CRDT",
				"err", err,
			)
			os.Exit(1)
		}

		// Check if just removed elements marked all
		// instances of mail file.
		if userMailboxCRDT.Lookup(storeUpd.RmvMail[0].Value) != true {

			// If that is the case, remove the file.
			err := os.Remove(delFileName)
			if err != nil {
				level.Error(node.Logger).Log(
					"msg", "failed to remove underlying mail file during downstream STORE execution",
					"err", err,
				)
				os.Exit(1)
			}
		}

		// Check if new mail name is not yet present
		// on this node.
		if userMailboxCRDT.Lookup(storeUpd.AddMail.Value) != true {

			// If not yet present on node, place file
			// content at correct location.
			storeFile, err := os.Create(storeFileName)
			if err != nil {
				level.Error(node.Logger).Log(
					"msg", "failed to create mail file during downstream STORE execution",
					"err", err,
				)
				os.Exit(1)
			}

			_, err = storeFile.Write(storeUpd.AddMail.Contents)
			if err != nil {

				level.Error(node.Logger).Log(
					"msg", "fail during downstream STORE execution, will clean up",
					"err", err,
				)

				// Remove just created mail file.
				err = os.Remove(storeFileName)
				if err != nil {
					level.Error(node.Logger).Log(
						"msg", "failed to remove created mail file",
						"err", err,
					)
				}

				os.Exit(1)
			}

			// Sync content to stable storage.
			err = storeFile.Sync()
			if err != nil {

				level.Error(node.Logger).Log(
					"msg", "fail during downstream STORE execution, will clean up",
					"err", err,
				)

				// Remove just created mail file.
				err = os.Remove(storeFileName)
				if err != nil {
					level.Error(node.Logger).Log(
						"msg", "failed to remove created mail file",
						"err", err,
					)
				}

				os.Exit(1)
			}

			// If succeeded, add renamed mail to mailbox' CRDT.
			err = userMailboxCRDT.AddEffect(storeUpd.AddMail.Value, storeUpd.AddMail.Tag, true)
			if err != nil {

				level.Error(node.Logger).Log(
					"msg", "fail during downstream STORE execution, will clean up",
					"err", err,
				)

				// Remove just created mail file.
				err = os.Remove(storeFileName)
				if err != nil {
					level.Error(node.Logger).Log(
						"msg", "failed to remove created mail file",
						"err", err,
					)
				}

				os.Exit(1)
			}
		} else {

			// Add renamed mail to mailbox' CRDT.
			err = userMailboxCRDT.AddEffect(storeUpd.AddMail.Value, storeUpd.AddMail.Tag, true)
			if err != nil {
				level.Error(node.Logger).Log(
					"msg", "fail during downstream STORE execution",
					"err", err,
				)
				os.Exit(1)
			}
		}

		for msgNum, msgName := range node.MailboxContents[storeUpd.User][storeUpd.Mailbox] {

			// Find old mail file's sequence number.
			if msgName == storeUpd.RmvMail[0].Value {

				// Replace old file name with renamed new one.
				node.MailboxContents[storeUpd.User][storeUpd.Mailbox][msgNum] = storeUpd.AddMail.Value
			}
		}
	}
}

// ApplyCRDTUpd receives strings representing CRDT
// update operations from receiver and executes them.
func (node *IMAPNode) ApplyCRDTUpd(applyChan <-chan comm.Msg, doneChan chan<- struct{}) {

	for {

		// Receive update message from receiver
		// via channel.
		msg := <-applyChan

		// Depending on received operation,
		// parse remaining payload further.
		switch msg.Operation {

		case "create":
			node.ApplyCreate(msg)

		case "delete":
			node.ApplyDelete(msg)

		case "append":
			node.ApplyAppend(msg)

		case "expunge":
			node.ApplyExpunge(msg)

		case "store":
			node.ApplyStore(msg)
		}

		// Signal receiver that update was performed.
		doneChan <- struct{}{}
	}
}
