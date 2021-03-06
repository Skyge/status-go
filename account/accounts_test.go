package account

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/golang/mock/gomock"
	. "github.com/status-im/status-go/t/utils"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestVerifyAccountPassword(t *testing.T) {
	accManager := NewManager(nil)
	keyStoreDir, err := ioutil.TempDir(os.TempDir(), "accounts")
	require.NoError(t, err)
	defer os.RemoveAll(keyStoreDir) //nolint: errcheck

	emptyKeyStoreDir, err := ioutil.TempDir(os.TempDir(), "accounts_empty")
	require.NoError(t, err)
	defer os.RemoveAll(emptyKeyStoreDir) //nolint: errcheck

	// import account keys
	require.NoError(t, ImportTestAccount(keyStoreDir, GetAccount1PKFile()))
	require.NoError(t, ImportTestAccount(keyStoreDir, GetAccount2PKFile()))

	account1Address := gethcommon.BytesToAddress(gethcommon.FromHex(TestConfig.Account1.Address))

	testCases := []struct {
		name          string
		keyPath       string
		address       string
		password      string
		expectedError error
	}{
		{
			"correct address, correct password (decrypt should succeed)",
			keyStoreDir,
			TestConfig.Account1.Address,
			TestConfig.Account1.Password,
			nil,
		},
		{
			"correct address, correct password, non-existent key store",
			filepath.Join(keyStoreDir, "non-existent-folder"),
			TestConfig.Account1.Address,
			TestConfig.Account1.Password,
			fmt.Errorf("cannot traverse key store folder: lstat %s/non-existent-folder: no such file or directory", keyStoreDir),
		},
		{
			"correct address, correct password, empty key store (pk is not there)",
			emptyKeyStoreDir,
			TestConfig.Account1.Address,
			TestConfig.Account1.Password,
			fmt.Errorf("cannot locate account for address: %s", account1Address.Hex()),
		},
		{
			"wrong address, correct password",
			keyStoreDir,
			"0x79791d3e8f2daa1f7fec29649d152c0ada3cc535",
			TestConfig.Account1.Password,
			fmt.Errorf("cannot locate account for address: %s", "0x79791d3E8F2dAa1F7FeC29649d152c0aDA3cc535"),
		},
		{
			"correct address, wrong password",
			keyStoreDir,
			TestConfig.Account1.Address,
			"wrong password", // wrong password
			errors.New("could not decrypt key with given passphrase"),
		},
	}
	for _, testCase := range testCases {
		accountKey, err := accManager.VerifyAccountPassword(testCase.keyPath, testCase.address, testCase.password)
		if !reflect.DeepEqual(err, testCase.expectedError) {
			require.FailNow(t, fmt.Sprintf("unexpected error: expected \n'%v', got \n'%v'", testCase.expectedError, err))
		}
		if err == nil {
			if accountKey == nil {
				require.Fail(t, "no error reported, but account key is missing")
			}
			accountAddress := gethcommon.BytesToAddress(gethcommon.FromHex(testCase.address))
			if accountKey.Address != accountAddress {
				require.Fail(t, "account mismatch: have %s, want %s", accountKey.Address.Hex(), accountAddress.Hex())
			}
		}
	}
}

// TestVerifyAccountPasswordWithAccountBeforeEIP55 verifies if VerifyAccountPassword
// can handle accounts before introduction of EIP55.
func TestVerifyAccountPasswordWithAccountBeforeEIP55(t *testing.T) {
	keyStoreDir, err := ioutil.TempDir("", "status-accounts-test")
	require.NoError(t, err)
	defer os.RemoveAll(keyStoreDir) //nolint: errcheck

	// Import keys and make sure one was created before EIP55 introduction.
	err = ImportTestAccount(keyStoreDir, "test-account3-before-eip55.pk")
	require.NoError(t, err)

	accManager := NewManager(nil)

	address := gethcommon.HexToAddress(TestConfig.Account3.Address)
	_, err = accManager.VerifyAccountPassword(keyStoreDir, address.Hex(), TestConfig.Account3.Password)
	require.NoError(t, err)
}

var (
	errKeyStore   = errors.New("Can't return a key store")
	errAccManager = errors.New("Can't return an account manager")
)

func TestManagerTestSuite(t *testing.T) {
	gethServiceProvider := newMockGethServiceProvider(t)
	accManager := NewManager(gethServiceProvider)

	keyStoreDir, err := ioutil.TempDir(os.TempDir(), "accounts")
	require.NoError(t, err)
	keyStore := keystore.NewKeyStore(keyStoreDir, keystore.LightScryptN, keystore.LightScryptP)
	defer os.RemoveAll(keyStoreDir) //nolint: errcheck

	testPassword := "test-password"

	// Initial test - create test account
	gethServiceProvider.EXPECT().AccountKeyStore().Return(keyStore, nil)
	addr, pubKey, mnemonic, err := accManager.CreateAccount(testPassword)
	require.NoError(t, err)
	require.NotEmpty(t, addr)
	require.NotEmpty(t, pubKey)
	require.NotEmpty(t, mnemonic)

	s := &ManagerTestSuite{
		testAccount: testAccount{
			"test-password",
			addr,
			pubKey,
			mnemonic,
		},
		gethServiceProvider: gethServiceProvider,
		accManager:          accManager,
		keyStore:            keyStore,
		gethAccManager:      accounts.NewManager(),
	}

	suite.Run(t, s)
}

func newMockGethServiceProvider(t *testing.T) *MockGethServiceProvider {
	ctrl := gomock.NewController(t)
	return NewMockGethServiceProvider(ctrl)
}

type ManagerTestSuite struct {
	suite.Suite
	testAccount
	gethServiceProvider *MockGethServiceProvider
	accManager          *Manager
	keyStore            *keystore.KeyStore
	gethAccManager      *accounts.Manager
}

type testAccount struct {
	password string
	address  string
	pubKey   string
	mnemonic string
}

// reinitMock is for reassigning a new mock node manager to account manager.
// Stating the amount of times for mock calls kills the flexibility for
// development so this is a good workaround to use with EXPECT().Func().AnyTimes()
func (s *ManagerTestSuite) reinitMock() {
	s.gethServiceProvider = newMockGethServiceProvider(s.T())
	s.accManager.geth = s.gethServiceProvider
}

// SetupTest is used here for reinitializing the mock before every
// test function to avoid faulty execution.
func (s *ManagerTestSuite) SetupTest() {
	s.reinitMock()
}

func (s *ManagerTestSuite) TestCreateAccount() {
	// Don't fail on empty password
	s.gethServiceProvider.EXPECT().AccountKeyStore().Return(s.keyStore, nil)
	_, _, _, err := s.accManager.CreateAccount(s.password)
	s.NoError(err)

	s.gethServiceProvider.EXPECT().AccountKeyStore().Return(nil, errKeyStore)
	_, _, _, err = s.accManager.CreateAccount(s.password)
	s.Equal(errKeyStore, err)
}

func (s *ManagerTestSuite) TestRecoverAccount() {
	s.gethServiceProvider.EXPECT().AccountKeyStore().Return(s.keyStore, nil)
	addr, pubKey, err := s.accManager.RecoverAccount(s.password, s.mnemonic)
	s.NoError(err)
	s.Equal(s.address, addr)
	s.Equal(s.pubKey, pubKey)

	s.gethServiceProvider.EXPECT().AccountKeyStore().Return(nil, errKeyStore)
	_, _, err = s.accManager.RecoverAccount(s.password, s.mnemonic)
	s.Equal(errKeyStore, err)
}

func (s *ManagerTestSuite) TestSelectAccount() {
	testCases := []struct {
		name                  string
		accountKeyStoreReturn []interface{}
		address               string
		password              string
		expectedError         error
	}{
		{
			"success",
			[]interface{}{s.keyStore, nil},
			s.address,
			s.password,
			nil,
		},
		{
			"fail_keyStore",
			[]interface{}{nil, errKeyStore},
			s.address,
			s.password,
			errKeyStore,
		},
		{
			"fail_wrongAddress",
			[]interface{}{s.keyStore, nil},
			"wrong-address",
			s.password,
			ErrAddressToAccountMappingFailure,
		},
		{
			"fail_wrongPassword",
			[]interface{}{s.keyStore, nil},
			s.address,
			"wrong-password",
			errors.New("cannot retrieve a valid key for a given account: could not decrypt key with given passphrase"),
		},
	}

	for _, testCase := range testCases {
		s.T().Run(testCase.name, func(t *testing.T) {
			s.reinitMock()
			s.gethServiceProvider.EXPECT().AccountKeyStore().Return(testCase.accountKeyStoreReturn...).AnyTimes()
			err := s.accManager.SelectAccount(testCase.address, testCase.password)
			s.Equal(testCase.expectedError, err)

			selectedWalletAccount, err := s.accManager.SelectedWalletAccount()
			s.NoError(err)
			selectedChatAccount, err := s.accManager.SelectedChatAccount()
			s.NoError(err)

			s.Equal(selectedWalletAccount.AccountKey, selectedChatAccount.AccountKey)
		})
	}
}

func (s *ManagerTestSuite) TestCreateChildAccount() {
	// First, test the negative case where an account is not selected
	// and an address is not provided.
	s.accManager.selectedWalletAccount = nil
	s.T().Run("fail_noAccount", func(t *testing.T) {
		s.gethServiceProvider.EXPECT().AccountKeyStore().Return(s.keyStore, nil).AnyTimes()
		_, _, err := s.accManager.CreateChildAccount("", s.password)
		s.Equal(ErrNoAccountSelected, err)
	})

	// Now, select the test account for rest of the test cases.
	s.reinitMock()
	s.gethServiceProvider.EXPECT().AccountKeyStore().Return(s.keyStore, nil).AnyTimes()
	err := s.accManager.SelectAccount(s.address, s.password)
	s.NoError(err)

	testCases := []struct {
		name                  string
		address               string
		password              string
		accountKeyStoreReturn []interface{}
		expectedError         error
	}{
		{
			"success",
			s.address,
			s.password,
			[]interface{}{s.keyStore, nil},
			nil,
		},
		{
			"fail_keyStore",
			s.address,
			s.password,
			[]interface{}{nil, errKeyStore},
			errKeyStore,
		},
		{
			"fail_wrongAddress",
			"wrong-address",
			s.password,
			[]interface{}{s.keyStore, nil},
			ErrAddressToAccountMappingFailure,
		},
		{
			"fail_wrongPassword",
			s.address,
			"wrong-password",
			[]interface{}{s.keyStore, nil},
			errors.New("cannot retrieve a valid key for a given account: could not decrypt key with given passphrase"),
		},
	}

	for _, testCase := range testCases {
		s.T().Run(testCase.name, func(t *testing.T) {
			s.reinitMock()
			s.gethServiceProvider.EXPECT().AccountKeyStore().Return(testCase.accountKeyStoreReturn...).AnyTimes()
			childAddr, childPubKey, err := s.accManager.CreateChildAccount(testCase.address, testCase.password)
			if testCase.expectedError != nil {
				s.Equal(testCase.expectedError, err)
			} else {
				s.NoError(err)
				s.NotEmpty(childAddr)
				s.NotEmpty(childPubKey)
			}
		})
	}
}

func (s *ManagerTestSuite) TestLogout() {
	s.accManager.Logout()
	s.Nil(s.accManager.selectedWalletAccount)
}

// TestAccounts tests cases for (*Manager).Accounts.
func (s *ManagerTestSuite) TestAccounts() {
	// Select the test account
	s.gethServiceProvider.EXPECT().AccountKeyStore().Return(s.keyStore, nil).AnyTimes()
	err := s.accManager.SelectAccount(s.address, s.password)
	s.NoError(err)

	// Success
	s.gethServiceProvider.EXPECT().AccountManager().Return(s.gethAccManager, nil)
	accs, err := s.accManager.Accounts()
	s.NoError(err)
	s.NotNil(accs)

	// Can't get an account manager
	s.gethServiceProvider.EXPECT().AccountManager().Return(nil, errAccManager)
	_, err = s.accManager.Accounts()
	s.Equal(errAccManager, err)

	// Selected account is nil but doesn't fail
	s.accManager.selectedWalletAccount = nil
	s.gethServiceProvider.EXPECT().AccountManager().Return(s.gethAccManager, nil)
	accs, err = s.accManager.Accounts()
	s.NoError(err)
	s.NotNil(accs)
}

func (s *ManagerTestSuite) TestAddressToDecryptedAccount() {
	testCases := []struct {
		name                  string
		accountKeyStoreReturn []interface{}
		address               string
		password              string
		expectedError         error
	}{
		{
			"success",
			[]interface{}{s.keyStore, nil},
			s.address,
			s.password,
			nil,
		},
		{
			"fail_keyStore",
			[]interface{}{nil, errKeyStore},
			s.address,
			s.password,
			errKeyStore,
		},
		{
			"fail_wrongAddress",
			[]interface{}{s.keyStore, nil},
			"wrong-address",
			s.password,
			ErrAddressToAccountMappingFailure,
		},
		{
			"fail_wrongPassword",
			[]interface{}{s.keyStore, nil},
			s.address,
			"wrong-password",
			errors.New("could not decrypt key with given passphrase"),
		},
	}

	for _, testCase := range testCases {
		s.T().Run(testCase.name, func(t *testing.T) {
			s.reinitMock()
			s.gethServiceProvider.EXPECT().AccountKeyStore().Return(testCase.accountKeyStoreReturn...).AnyTimes()
			acc, key, err := s.accManager.AddressToDecryptedAccount(testCase.address, testCase.password)
			if testCase.expectedError != nil {
				s.Equal(testCase.expectedError, err)
			} else {
				s.NoError(err)
				s.NotNil(acc)
				s.NotNil(key)
				s.Equal(acc.Address, key.Address)
			}
		})
	}
}
