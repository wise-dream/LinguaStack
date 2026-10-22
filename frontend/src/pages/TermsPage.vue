<script setup lang="ts">
// Условия и конфиденциальность закрытой беты (GDPR: сервер в ЕС). Каждое
// утверждение — о том, как сервис устроен на самом деле; меняется код —
// меняется и эта страница (дополнение §18).
import { useSession } from '../session'

const session = useSession()
</script>

<template>
  <main class="page terms">
    <!-- Без входа шапки нет: путь обратно — название сервиса, как на странице входа. -->
    <RouterLink
      v-if="!session.me.value"
      to="/"
      class="back"
    >
      LinguaStack
    </RouterLink>
    <h1>Условия и конфиденциальность</h1>
    <p class="lead">
      LinguaStack — бета-версия: сервис не рекламируется, о нём знают те, кому дали ссылку. Он переводит книги и документацию
      моделями, которые вы подключаете сами, и показывает оригинал рядом с переводом. Возможны ошибки и перерывы
      в работе — храните оригиналы книг у себя.
    </p>

    <section aria-labelledby="terms-data">
      <h2 id="terms-data">
        Что хранится
      </h2>
      <ul>
        <li>Аккаунт: адрес почты и пароль в виде хеша (argon2id) — сам пароль не хранится.</li>
        <li>Ваши книги: загруженные файлы, их разобранный текст и картинки.</li>
        <li>Переводы, ваши правки, файлы экспорта и место, где вы остановились в чтении.</li>
        <li>
          Подключения к моделям. Ключи API хранятся зашифрованными и после сохранения не показываются никому,
          в том числе администратору.
        </li>
        <li>Привязка Telegram, если вы её сделали: номер чата и имя пользователя в Telegram.</li>
        <li>Журнал попыток входа: адрес почты, IP-адрес и время — для защиты от подбора пароля.</li>
      </ul>
      <p>
        В браузере — cookie сессии (только чтобы вы оставались в аккаунте) и выбранная тема оформления.
        Рекламных и аналитических cookie нет.
      </p>
    </section>

    <section aria-labelledby="terms-where">
      <h2 id="terms-where">
        Где
      </h2>
      <p>
        Сервер стоит в дата-центре Hetzner в Хельсинки (Финляндия). Резервные копии базы и файлов делаются раз в
        сутки на том же сервере и хранятся 14 дней.
      </p>
    </section>

    <section aria-labelledby="terms-third">
      <h2 id="terms-third">
        Кому передаётся текст
      </h2>
      <ul>
        <li>
          Текст книги уходит только в сервис моделей, который вы подключили сами (Anthropic или
          OpenAI-совместимый), с вашим ключом и на условиях этого сервиса. Перед первым переводом через внешний
          сервис интерфейс спрашивает разрешение.
        </li>
        <li>Локальные модели сервера (Ollama) работают на этом же сервере — текст его не покидает.</li>
        <li>
          В Telegram уходят уведомления с названиями книг и глав и ходом перевода — только если вы его
          привязали.
        </li>
        <li>
          Администратор видит в своём разделе адрес почты, даты, число книг и переводов и занятое место, но не
          содержимое ваших книг и переводов.
        </li>
      </ul>
    </section>

    <section aria-labelledby="terms-rights">
      <h2 id="terms-rights">
        Ваши книги и права на них
      </h2>
      <p>
        Загружайте только то, на что у вас есть права: свои тексты, книги под открытыми лицензиями, книги для
        личного чтения. Переводы — для вас: сервис ничего не публикует и не показывает другим пользователям.
      </p>
    </section>

    <section aria-labelledby="terms-delete">
      <h2 id="terms-delete">
        Удаление данных
      </h2>
      <p>
        <!-- eslint-disable-next-line vue/singleline-html-element-content-newline -- перенос внутри ссылки дал бы лишний подчёркнутый пробел -->
        Аккаунт со всеми данными удаляется сразу: <RouterLink to="/settings">Настройки</RouterLink> → «Удаление аккаунта». Из резервных копий данные уходят по мере их смены — не позже чем
        через 14 дней. Отдельную книгу вместе с её переводами можно удалить в библиотеке.
      </p>
    </section>

    <section aria-labelledby="terms-contact">
      <h2 id="terms-contact">
        Вопросы
      </h2>
      <p>Пишите администратору — тому, кто дал вам ссылку на сервис.</p>
    </section>

    <p class="muted">
      Действует с 22 октября 2026 г.
    </p>
  </main>
</template>

<style scoped>
/* Текст для чтения — в удобную строку, а не во всю ширину страницы. */
.terms {
  max-width: var(--measure);
}

.back {
  display: inline-block;
  margin-bottom: var(--space-4);
  font-family: var(--font-book);
  font-size: var(--step-1);
  font-weight: 600;
  text-decoration: none;
}

.lead {
  font-family: var(--font-book);
  font-size: var(--step-1);
  line-height: 1.5;
  margin-bottom: var(--space-6);
}

section + section {
  margin-top: var(--space-5);
}

h2 {
  font-size: var(--step-1);
  margin-bottom: var(--space-2);
}

ul {
  display: grid;
  gap: var(--space-2);
  margin: 0 0 var(--space-3);
  padding-left: var(--space-5);
}

p {
  margin: 0 0 var(--space-3);
}

.muted {
  margin-top: var(--space-6);
  color: var(--muted);
}
</style>
